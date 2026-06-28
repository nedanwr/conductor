package repostorage

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/http2"

	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	"github.com/nedanwr/conductor/git-server/internal/core/repostorage"
	"github.com/nedanwr/conductor/git-server/internal/gen/gitserver/repostorage/v1/repostoragev1connect"
)

// h2cServer mounts the Connect handler over cleartext HTTP/2 (h2c), which Connect
// bidi streaming requires, and returns the base URL plus a client wired to speak
// h2c to it.
func h2cServer(t *testing.T, impl repostorage.RepoStorage) (string, *http.Client) {
	t.Helper()
	path, handler := repostoragev1connect.NewRepoStorageServiceHandler(NewConnectHandler(impl))
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

// advertise sends a single flush packet over Fetch, eliciting the deterministic
// ref advertisement and a clean upload-pack exit. It runs against any
// RepoStorage — the in-process impl or the remote client adapter.
func advertise(t *testing.T, impl repostorage.RepoStorage, repoID string) []byte {
	t.Helper()
	var out bytes.Buffer
	req := gitreq.GitRequest{RepoID: repoID, Operation: gitreq.OperationFetch, Protocol: gitreq.ProtocolParams{Version: 2}}
	if err := impl.Fetch(context.Background(), req, bytes.NewReader([]byte("0000")), &out); err != nil {
		t.Fatalf("fetch advertisement: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("empty ref advertisement")
	}
	return out.Bytes()
}

// TestLocationTransparency proves the seam: the same request against the
// in-process Store and against its Connect client over loopback yields identical
// results, so the multi-binary split is a validation, not a behavior change.
func TestLocationTransparency(t *testing.T) {
	store := NewStore(t.TempDir(), newRunner(t))
	repoID := "99999999-8888-7777-6666-555555555555"
	seedRepo(t, store, repoID)

	url, httpClient := h2cServer(t, store)
	client := NewConnectClient(httpClient, url)

	// CreateRepo round-trips identically through the remote path.
	if err := client.CreateRepo(context.Background(), "12121212-3434-5656-7878-909090909090", "main"); err != nil {
		t.Fatalf("remote CreateRepo: %v", err)
	}

	local := advertise(t, store, repoID)
	remote := advertise(t, client, repoID)
	if !bytes.Equal(local, remote) {
		t.Fatalf("advertisement differs between in-process and remote:\nlocal:  %q\nremote: %q", local, remote)
	}
}
