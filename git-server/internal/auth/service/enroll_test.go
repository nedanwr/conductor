package service_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nedanwr/conductor/git-server/internal/auth/service"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	enrollv1connect "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/enroll/v1/enrollv1connect"
	"github.com/nedanwr/conductor/git-server/internal/transport"
)

// secretIssuer is a test trust anchor: it signs identities from a CA but only for
// callers presenting the expected bootstrap token, mirroring how the real
// registry-backed anchor gates enrollment.
type secretIssuer struct {
	ca    *service.CA
	token string
}

func (i secretIssuer) Issue(_ context.Context, p service.EnrollParams) (service.EnrollResult, error) {
	if p.Token != i.token {
		return service.EnrollResult{}, giterr.Unauthorized("bad token")
	}
	leaf, notAfter, err := i.ca.IssueFromCSR(p.CSR, p.Name, time.Hour)
	if err != nil {
		return service.EnrollResult{}, err
	}
	return service.EnrollResult{LeafDER: leaf, RootDER: i.ca.RootDER(), NotAfter: notAfter}, nil
}

// TestEnrollRoundTrip drives a node through the bootstrap endpoint: with the
// right token it receives working identity bound to the name it asked for, and
// with the wrong token it is refused as unauthorized — the typed error surviving
// the Connect boundary.
func TestEnrollRoundTrip(t *testing.T) {
	ca, err := service.NewCA()
	if err != nil {
		t.Fatal(err)
	}
	const token = "s3cret-bootstrap"

	path, handler := enrollv1connect.NewEnrollServiceHandler(
		service.NewEnrollHandler(secretIssuer{ca: ca, token: token}),
	)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	srv := httptest.NewServer(transport.H2CHandler(mux))
	defer srv.Close()

	client := transport.NewH2CClient()
	name := service.Name{Role: "cache", NodeID: "cache-3"}

	mat, err := service.Enroll(context.Background(), client, srv.URL, token, name)
	if err != nil {
		t.Fatalf("Enroll with valid token: %v", err)
	}
	if mat.Name() != name {
		t.Fatalf("enrolled identity = %+v, want %+v", mat.Name(), name)
	}

	_, err = service.Enroll(context.Background(), client, srv.URL, "wrong", name)
	if err == nil {
		t.Fatal("Enroll with wrong token unexpectedly succeeded")
	}
	if got := giterr.KindOf(err); got != giterr.KindUnauthorized {
		t.Fatalf("wrong-token error kind = %v, want Unauthorized", got)
	}
}
