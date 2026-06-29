package service

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestNameRoundTrips confirms a Name encodes to a SPIFFE-style URI and parses
// back unchanged, and that an identity outside the trust domain is rejected.
func TestNameRoundTrips(t *testing.T) {
	want := Name{Role: "gateway", NodeID: "node-7"}
	got, err := ParseName(want.URI())
	if err != nil {
		t.Fatalf("ParseName: %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}

	foreign := want.URI()
	foreign.Host = "evil"
	if _, err := ParseName(foreign); err == nil {
		t.Fatal("ParseName admitted an identity from a foreign trust domain")
	}
}

// TestCAIssuesVerifiableLeaf confirms the CA signs a leaf for a CSR that chains
// to its root and carries the assigned identity, and that a garbage CSR fails.
func TestCAIssuesVerifiableLeaf(t *testing.T) {
	ca := newTestCA(t)
	name := Name{Role: "repo-storage", NodeID: "store-1"}
	_, csr, err := NewKeyAndCSR(name)
	if err != nil {
		t.Fatal(err)
	}

	leafDER, notAfter, err := ca.IssueFromCSR(csr, name, time.Hour)
	if err != nil {
		t.Fatalf("IssueFromCSR: %v", err)
	}
	if time.Until(notAfter) <= 0 {
		t.Fatalf("issued leaf already expired: notAfter=%s", notAfter)
	}
	mat, err := NewMaterial(nil, leafDER, ca.RootDER())
	if err != nil {
		t.Fatalf("NewMaterial: %v", err)
	}
	if mat.Name() != name {
		t.Fatalf("leaf identity = %+v, want %+v", mat.Name(), name)
	}

	if _, _, err := ca.IssueFromCSR([]byte("not a csr"), name, time.Hour); err == nil {
		t.Fatal("IssueFromCSR accepted a malformed CSR")
	}
}

// TestMTLSAuthenticatesPeers proves two nodes enrolled under one CA mutually
// authenticate and each recovers the other's identity, while a node holding a
// leaf from a different CA is refused at the handshake.
func TestMTLSAuthenticatesPeers(t *testing.T) {
	ca := newTestCA(t)
	gateway := selfIssue(t, ca, Name{Role: "gateway", NodeID: "gw-1"})
	storage := selfIssue(t, ca, Name{Role: "repo-storage", NodeID: "st-1"})

	serverState, clientState, err := handshake(t, storage.ServerTLSConfig(), gateway.ClientTLSConfig())
	if err != nil {
		t.Fatalf("mutual handshake failed: %v", err)
	}
	if got := peerName(t, serverState); got != gateway.Name() {
		t.Fatalf("server saw caller %+v, want %+v", got, gateway.Name())
	}
	if got := peerName(t, clientState); got != storage.Name() {
		t.Fatalf("client saw peer %+v, want %+v", got, storage.Name())
	}

	// A leaf from an unrelated CA is not trusted by this deployment's root.
	stranger := selfIssue(t, newTestCA(t), Name{Role: "gateway", NodeID: "rogue"})
	if _, _, err := handshake(t, storage.ServerTLSConfig(), stranger.ClientTLSConfig()); err == nil {
		t.Fatal("handshake admitted a peer from an untrusted CA")
	}
}

// TestServerMiddlewareRequiresIdentity confirms the boundary middleware admits a
// request only when an anchor resolves a verified peer: the no-op anchor lets a
// plain request through, while the peer anchor rejects one carrying no identity.
func TestServerMiddlewareRequiresIdentity(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	rec := httptest.NewRecorder()
	ServerMiddleware(NoopAnchor{}, ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("noop anchor blocked request: status %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	ServerMiddleware(PeerAnchor{}, ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("peer anchor admitted an unauthenticated request: status %d", rec.Code)
	}
}

// newTestCA builds a CA or fails the test.
func newTestCA(t *testing.T) *CA {
	t.Helper()
	ca, err := NewCA()
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

// selfIssue mints material for name under ca or fails the test.
func selfIssue(t *testing.T, ca *CA, name Name) *Material {
	t.Helper()
	mat, err := SelfIssue(ca, name, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return mat
}

// peerName extracts the verified peer identity from a connection state.
func peerName(t *testing.T, cs *tls.ConnectionState) Name {
	t.Helper()
	if len(cs.PeerCertificates) == 0 {
		t.Fatal("connection carried no peer certificate")
	}
	name, err := nameFromCert(cs.PeerCertificates[0])
	if err != nil {
		t.Fatal(err)
	}
	return name
}

// handshake performs an in-memory mutual TLS handshake between the two configs
// and returns each side's resulting connection state.
func handshake(t *testing.T, serverCfg, clientCfg *tls.Config) (*tls.ConnectionState, *tls.ConnectionState, error) {
	t.Helper()
	sconn, cconn := net.Pipe()
	deadline := time.Now().Add(5 * time.Second)
	_ = sconn.SetDeadline(deadline)
	_ = cconn.SetDeadline(deadline)
	defer sconn.Close()
	defer cconn.Close()

	srv := tls.Server(sconn, serverCfg)
	cli := tls.Client(cconn, clientCfg)

	errc := make(chan error, 1)
	go func() { errc <- srv.Handshake() }()
	cerr := cli.Handshake()
	serr := <-errc
	if cerr != nil {
		return nil, nil, cerr
	}
	if serr != nil {
		return nil, nil, serr
	}
	ss := srv.ConnectionState()
	cs := cli.ConnectionState()
	return &ss, &cs, nil
}
