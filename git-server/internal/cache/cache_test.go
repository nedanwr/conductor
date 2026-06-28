package cache

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/http2"

	"github.com/nedanwr/conductor/git-server/internal/core/cache"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	"github.com/nedanwr/conductor/git-server/internal/gen/gitserver/cache/v1/cachev1connect"
)

// fakeStorage is a stand-in RepoStorage that records the Fetch it sees and echoes
// the client input back as pack output, so a passthrough can be verified without
// a real git repo. Only Fetch is exercised; the write methods are unused.
type fakeStorage struct {
	gotReq   gitreq.GitRequest
	gotInput []byte
	reply    []byte
}

func (s *fakeStorage) CreateRepo(context.Context, string, string) error { return nil }

func (s *fakeStorage) Fetch(_ context.Context, req gitreq.GitRequest, r io.Reader, w io.Writer) error {
	s.gotReq = req
	in, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.gotInput = in
	_, err = w.Write(s.reply)
	return err
}

func (s *fakeStorage) Receive(context.Context, gitreq.GitRequest, io.Reader, io.Writer) error {
	return nil
}

var sampleReq = gitreq.GitRequest{
	RepoID:        "11111111-2222-3333-4444-555555555555",
	Operation:     gitreq.OperationFetch,
	Protocol:      gitreq.ProtocolParams{Version: 2, Capabilities: []string{"ofs-delta"}},
	CorrelationID: "corr-1",
}

// TestPassthrough proves the in-process cache forwards the request, the client
// input, and the pack output straight through to Repo Storage unchanged.
func TestPassthrough(t *testing.T) {
	storage := &fakeStorage{reply: []byte("PACKDATA")}
	c := New(storage)

	var out bytes.Buffer
	if err := c.Fetch(context.Background(), sampleReq, bytes.NewReader([]byte("client-bytes")), &out); err != nil {
		t.Fatalf("cache fetch: %v", err)
	}
	if string(storage.gotInput) != "client-bytes" {
		t.Fatalf("storage input = %q, want client-bytes", storage.gotInput)
	}
	if storage.gotReq.RepoID != sampleReq.RepoID || storage.gotReq.CorrelationID != sampleReq.CorrelationID {
		t.Fatalf("storage req = %+v, want passthrough of %+v", storage.gotReq, sampleReq)
	}
	if out.String() != "PACKDATA" {
		t.Fatalf("output = %q, want PACKDATA", out.String())
	}
}

// h2cServer mounts the Connect handler over cleartext HTTP/2 (h2c), which Connect
// bidi streaming requires, and returns the base URL plus a client wired for it.
func h2cServer(t *testing.T, impl cache.Cache) (string, *http.Client) {
	t.Helper()
	path, handler := cachev1connect.NewCacheServiceHandler(NewConnectHandler(impl))
	mux := http.NewServeMux()
	mux.Handle(path, handler)

	srv := httptest.NewUnstartedServer(mux)
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	srv.Config.Protocols = protocols
	srv.Start()
	t.Cleanup(srv.Close)

	client := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		},
	}
	return srv.URL, client
}

// TestLocationTransparency proves the seam: the same Fetch against the in-process
// cache and against its Connect client over loopback yields identical pack
// output, so the multi-binary split is a validation, not a behavior change.
func TestLocationTransparency(t *testing.T) {
	impl := New(&fakeStorage{reply: []byte("PACKDATA")})

	var local bytes.Buffer
	if err := impl.Fetch(context.Background(), sampleReq, bytes.NewReader([]byte("client-bytes")), &local); err != nil {
		t.Fatalf("in-process fetch: %v", err)
	}

	url, httpClient := h2cServer(t, New(&fakeStorage{reply: []byte("PACKDATA")}))
	client := NewConnectClient(httpClient, url)

	var remote bytes.Buffer
	if err := client.Fetch(context.Background(), sampleReq, bytes.NewReader([]byte("client-bytes")), &remote); err != nil {
		t.Fatalf("remote fetch: %v", err)
	}

	if !bytes.Equal(local.Bytes(), remote.Bytes()) {
		t.Fatalf("output differs between in-process and remote:\nlocal:  %q\nremote: %q", local.Bytes(), remote.Bytes())
	}
}
