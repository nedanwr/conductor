package gateway

import (
	"context"
	"io"
	"testing"

	"github.com/google/uuid"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	coreregistry "github.com/nedanwr/conductor/git-server/internal/core/registry"
)

// fakeResolver resolves any owner/repo to a fixed UUID, or returns a preset error.
type fakeResolver struct {
	id  uuid.UUID
	err error
}

func (f fakeResolver) Resolve(context.Context, string, string) (uuid.UUID, error) {
	return f.id, f.err
}

// fakeAuthorizer returns a preset grant or denial and records what it was asked.
type fakeAuthorizer struct {
	grant gitreq.Grant
	err   error

	gotPrincipal auth.User
	gotRepo      uuid.UUID
	gotOp        gitreq.Operation
}

func (f *fakeAuthorizer) Can(_ context.Context, p auth.User, repo uuid.UUID, op gitreq.Operation) (gitreq.Grant, error) {
	f.gotPrincipal, f.gotRepo, f.gotOp = p, repo, op
	return f.grant, f.err
}

// fakeRegistry resolves placement to a single node.
type fakeRegistry struct {
	node coreregistry.Node
	err  error
}

func (f fakeRegistry) ResolvePlacement(context.Context, string) (coreregistry.Node, error) {
	return f.node, f.err
}

func (f fakeRegistry) CreatePlacement(context.Context, string, string) (coreregistry.Node, error) {
	return f.node, f.err
}

// captureStore records the request it was handed and which method ran.
type captureStore struct {
	req    gitreq.GitRequest
	method string
}

func (c *captureStore) CreateRepo(context.Context, string, string) error { return nil }

func (c *captureStore) Fetch(_ context.Context, req gitreq.GitRequest, _ io.Reader, _ io.Writer) error {
	c.req, c.method = req, "fetch"
	return nil
}

func (c *captureStore) Receive(_ context.Context, req gitreq.GitRequest, _ io.Reader, _ io.Writer) error {
	c.req, c.method = req, "receive"
	return nil
}

func newGateway(t *testing.T, resolver RepoResolver, authz Authorizer, store *captureStore) *Gateway {
	t.Helper()
	node := coreregistry.Node{ID: "node-1"}
	return New(resolver, authz, fakeRegistry{node: node}, NewSingleRouter("node-1", store))
}

func TestServeRoutesFetchWithDecidedGrant(t *testing.T) {
	repoID := uuid.New()
	authz := &fakeAuthorizer{grant: gitreq.Grant{Level: gitreq.GrantLevelRead}}
	store := &captureStore{}
	gw := newGateway(t, fakeResolver{id: repoID}, authz, store)

	in := Intake{
		Owner:         "alice",
		Repo:          "proj",
		Operation:     gitreq.OperationFetch,
		Principal:     auth.User{ID: uuid.New(), Username: "alice"},
		Protocol:      gitreq.ProtocolParams{Version: 2},
		CorrelationID: "corr-1",
	}
	if err := gw.Serve(context.Background(), in, nil, io.Discard); err != nil {
		t.Fatalf("Serve: %v", err)
	}

	if store.method != "fetch" {
		t.Fatalf("method = %q, want fetch", store.method)
	}
	if store.req.RepoID != repoID.String() {
		t.Errorf("RepoID = %q, want %q", store.req.RepoID, repoID)
	}
	if store.req.Grant.Level != gitreq.GrantLevelRead {
		t.Errorf("Grant = %v, want read", store.req.Grant.Level)
	}
	if store.req.CorrelationID != "corr-1" {
		t.Errorf("CorrelationID = %q, want corr-1", store.req.CorrelationID)
	}
	if authz.gotRepo != repoID || authz.gotOp != gitreq.OperationFetch {
		t.Errorf("authorizer asked about (%v,%v), want (%v,fetch)", authz.gotRepo, authz.gotOp, repoID)
	}
}

func TestServeRejectsUnauthorizedBeforeStorage(t *testing.T) {
	store := &captureStore{}
	authz := &fakeAuthorizer{err: giterr.Unauthorized("push denied")}
	gw := newGateway(t, fakeResolver{id: uuid.New()}, authz, store)

	in := Intake{Owner: "a", Repo: "b", Operation: gitreq.OperationReceive, Principal: auth.Anonymous}
	err := gw.Serve(context.Background(), in, nil, io.Discard)
	if giterr.KindOf(err) != giterr.KindUnauthorized {
		t.Fatalf("err kind = %v, want Unauthorized", giterr.KindOf(err))
	}
	if store.method != "" {
		t.Errorf("storage was called (%q) despite denial", store.method)
	}
}

func TestServePropagatesRepoNotFound(t *testing.T) {
	store := &captureStore{}
	gw := newGateway(t, fakeResolver{err: giterr.RepoNotFound("nope")}, &fakeAuthorizer{}, store)

	err := gw.Serve(context.Background(), Intake{Operation: gitreq.OperationFetch}, nil, io.Discard)
	if giterr.KindOf(err) != giterr.KindRepoNotFound {
		t.Fatalf("err kind = %v, want RepoNotFound", giterr.KindOf(err))
	}
	if store.method != "" {
		t.Errorf("storage was called despite missing repo")
	}
}

// TestNormalizeErasesTransport proves the boundary payload's identity and
// authorization fields are identical whether the request arrived over a stateful
// (SSH-style) or stateless (HTTPS-style) edge — the only difference is the git
// wire shape, which both transports legitimately carry.
func TestNormalizeErasesTransport(t *testing.T) {
	repoID := uuid.New()
	authz := &fakeAuthorizer{grant: gitreq.Grant{Level: gitreq.GrantLevelWrite}}
	principal := auth.User{ID: uuid.New(), Username: "alice"}

	ssh := Intake{
		Owner: "alice", Repo: "proj", Operation: gitreq.OperationFetch,
		Principal: principal, Protocol: gitreq.ProtocolParams{Version: 2},
		CorrelationID: "corr",
	}
	https := ssh
	https.Protocol.Stateless = true // the HTTP edge marks the wire shape

	sshStore, httpsStore := &captureStore{}, &captureStore{}
	if err := newGateway(t, fakeResolver{id: repoID}, authz, sshStore).Serve(context.Background(), ssh, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := newGateway(t, fakeResolver{id: repoID}, authz, httpsStore).Serve(context.Background(), https, nil, io.Discard); err != nil {
		t.Fatal(err)
	}

	a, b := sshStore.req, httpsStore.req
	if a.RepoID != b.RepoID || a.Operation != b.Operation || a.Grant != b.Grant ||
		a.Principal != b.Principal || a.Protocol.Version != b.Protocol.Version ||
		a.CorrelationID != b.CorrelationID {
		t.Errorf("transport-agnostic fields differ:\n ssh=%+v\n http=%+v", a, b)
	}
}

func TestNormalizeMintsCorrelationID(t *testing.T) {
	store := &captureStore{}
	gw := newGateway(t, fakeResolver{id: uuid.New()}, &fakeAuthorizer{}, store)
	in := Intake{Operation: gitreq.OperationFetch}
	if err := gw.Serve(context.Background(), in, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(store.req.CorrelationID); err != nil {
		t.Errorf("minted correlation id %q is not a uuid: %v", store.req.CorrelationID, err)
	}
}

func TestAnonymousPrincipalCarriesNoUserID(t *testing.T) {
	store := &captureStore{}
	gw := newGateway(t, fakeResolver{id: uuid.New()}, &fakeAuthorizer{}, store)
	in := Intake{Operation: gitreq.OperationFetch, Principal: auth.Anonymous}
	if err := gw.Serve(context.Background(), in, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !store.req.Principal.Anonymous || store.req.Principal.UserID != "" {
		t.Errorf("anonymous principal leaked id: %+v", store.req.Principal)
	}
}

func TestOperationForService(t *testing.T) {
	for _, tc := range []struct {
		service string
		want    gitreq.Operation
		ok      bool
	}{
		{"git-upload-pack", gitreq.OperationFetch, true},
		{"git-receive-pack", gitreq.OperationReceive, true},
		{"git-bogus", gitreq.OperationUnspecified, false},
	} {
		got, err := OperationForService(tc.service)
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected err %v", tc.service, err)
		}
		if !tc.ok && giterr.KindOf(err) != giterr.KindUnauthorized {
			t.Errorf("%s: err kind = %v, want Unauthorized", tc.service, giterr.KindOf(err))
		}
		if got != tc.want {
			t.Errorf("%s: op = %v, want %v", tc.service, got, tc.want)
		}
	}
}
