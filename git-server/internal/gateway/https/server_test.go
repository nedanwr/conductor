package https

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	coreregistry "github.com/nedanwr/conductor/git-server/internal/core/registry"
	"github.com/nedanwr/conductor/git-server/internal/gateway"
)

type fakeResolver struct{ id uuid.UUID }

func (f fakeResolver) Resolve(context.Context, string, string) (uuid.UUID, error) {
	return f.id, nil
}

// fakeAuthorizer grants read/write to a named user and denies the anonymous
// principal, so the tests can exercise both the public and the rejected paths.
type fakeAuthorizer struct{}

func (fakeAuthorizer) Can(_ context.Context, p auth.User, _ uuid.UUID, op gitreq.Operation) (gitreq.Grant, error) {
	if p.Anonymous {
		return gitreq.Grant{}, giterr.Unauthorized("auth required")
	}
	return gitreq.Grant{Level: gitreq.GrantLevelWrite}, nil
}

type fakeRegistry struct{}

func (fakeRegistry) ResolvePlacement(context.Context, string) (coreregistry.Node, error) {
	return coreregistry.Node{ID: "node-1"}, nil
}
func (fakeRegistry) CreatePlacement(context.Context, string, string) (coreregistry.Node, error) {
	return coreregistry.Node{ID: "node-1"}, nil
}

// echoStore records the request shape and writes a marker so the test can see
// the response body stream through.
type echoStore struct{ last gitreq.GitRequest }

func (s *echoStore) CreateRepo(context.Context, string, string) error { return nil }
func (s *echoStore) Fetch(_ context.Context, req gitreq.GitRequest, _ io.Reader, w io.Writer) error {
	s.last = req
	_, err := io.WriteString(w, "PACK-DATA")
	return err
}
func (s *echoStore) Receive(_ context.Context, req gitreq.GitRequest, _ io.Reader, w io.Writer) error {
	s.last = req
	_, err := io.WriteString(w, "REPORT")
	return err
}

type fakeToken struct{ user auth.User }

func (f fakeToken) FromToken(context.Context, string) (auth.User, error) { return f.user, nil }

func newHandler(t *testing.T) (*Handler, *echoStore) {
	t.Helper()
	store := &echoStore{}
	gw := gateway.New(fakeResolver{id: uuid.New()}, fakeAuthorizer{}, fakeRegistry{}, gateway.NewSingleRouter("node-1", store))
	authn := fakeToken{user: auth.User{ID: uuid.New(), Username: "alice"}}
	return NewHandler(gw, authn), store
}

func TestAdvertiseEmitsServiceHeaderAndAdvertiseShape(t *testing.T) {
	h, store := newHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/alice/proj.git/info/refs?service=git-upload-pack", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Git-Protocol", "version=2")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-git-upload-pack-advertisement" {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), "001e# service=git-upload-pack\n0000") {
		t.Errorf("body missing service preamble: %q", rec.Body.String()[:40])
	}
	if !strings.HasSuffix(rec.Body.String(), "PACK-DATA") {
		t.Errorf("advertisement body not streamed: %q", rec.Body.String())
	}
	if !store.last.Protocol.Stateless || !store.last.Protocol.AdvertiseRefs {
		t.Errorf("advertise did not set stateless+advertise shape: %+v", store.last.Protocol)
	}
	if store.last.Protocol.Version != 2 {
		t.Errorf("protocol version = %d, want 2", store.last.Protocol.Version)
	}
}

func TestRPCUploadPackUsesStatelessShape(t *testing.T) {
	h, store := newHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/alice/proj.git/git-upload-pack", strings.NewReader("want"))
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-git-upload-pack-result" {
		t.Errorf("content-type = %q", ct)
	}
	if rec.Body.String() != "PACK-DATA" {
		t.Errorf("body = %q, want PACK-DATA", rec.Body.String())
	}
	if !store.last.Protocol.Stateless || store.last.Protocol.AdvertiseRefs {
		t.Errorf("rpc shape wrong: %+v", store.last.Protocol)
	}
}

func TestAnonymousDeniedPromptsCredentials(t *testing.T) {
	h, _ := newHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/alice/proj.git/info/refs?service=git-upload-pack", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Errorf("missing WWW-Authenticate challenge")
	}
}

func TestUnknownEndpointIs404(t *testing.T) {
	h, _ := newHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/alice/proj.git/objects/info/packs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestParsePath(t *testing.T) {
	for _, tc := range []struct {
		path                  string
		owner, repo, endpoint string
		ok                    bool
	}{
		{"/alice/proj.git/info/refs", "alice", "proj", "info/refs", true},
		{"/alice/proj.git/git-upload-pack", "alice", "proj", "git-upload-pack", true},
		{"/proj.git/info/refs", "", "", "", false},
		{"/a/b/c.git/info/refs", "", "", "", false},
		{"/no-dot-git/info/refs", "", "", "", false},
	} {
		owner, repo, endpoint, ok := parsePath(tc.path)
		if ok != tc.ok || owner != tc.owner || repo != tc.repo || endpoint != tc.endpoint {
			t.Errorf("parsePath(%q) = (%q,%q,%q,%v), want (%q,%q,%q,%v)",
				tc.path, owner, repo, endpoint, ok, tc.owner, tc.repo, tc.endpoint, tc.ok)
		}
	}
}

func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

func TestBearerOrBasic(t *testing.T) {
	basic := "Basic " + basicAuth("git", "secret")
	for _, tc := range []struct {
		header string
		want   string
		ok     bool
	}{
		{"", "", false},
		{"Bearer abc", "abc", true},
		{basic, "secret", true},
		{"Basic !!!notbase64", "", false},
		{"Negotiate xyz", "", false},
	} {
		got, ok := bearerOrBasic(tc.header)
		if got != tc.want || ok != tc.ok {
			t.Errorf("bearerOrBasic(%q) = (%q,%v), want (%q,%v)", tc.header, got, ok, tc.want, tc.ok)
		}
	}
}
