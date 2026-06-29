package service

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"time"
)

// SelfIssue mints Material directly from a CA with no network round trip. It is
// for the one node that holds the CA itself — the trust anchor's own process —
// which enrolls itself rather than calling the bootstrap endpoint it serves.
func SelfIssue(ca *CA, name Name, ttl time.Duration) (*Material, error) {
	key, csr, err := NewKeyAndCSR(name)
	if err != nil {
		return nil, err
	}
	leaf, _, err := ca.IssueFromCSR(csr, name, ttl)
	if err != nil {
		return nil, err
	}
	return NewMaterial(key, leaf, ca.RootDER())
}

// NewKeyAndCSR generates a node's private key and a certificate-signing request
// to present at enrollment. The key never leaves the node; only the CSR (which
// carries the public half) crosses the wire, so the enrollment authority signs
// an identity the node alone can wield. The CSR subject is advisory — the CA
// assigns the real name — but is filled in for readability.
func NewKeyAndCSR(name Name) (ed25519.PrivateKey, []byte, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("service identity: generate key: %w", err)
	}
	tmpl := &x509.CertificateRequest{Subject: pkix.Name{CommonName: name.String()}}
	csr, err := x509.CreateCertificateRequest(rand.Reader, tmpl, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("service identity: create csr: %w", err)
	}
	return priv, csr, nil
}

// Material is a node's working identity: the leaf certificate proving who it is,
// the private key backing that proof, and the trust root used to verify the
// peers it speaks to. It is the product of enrollment and the input to every
// mutually-authenticated connection the node makes or accepts.
type Material struct {
	cert  tls.Certificate
	name  Name
	roots *x509.CertPool
}

// NewMaterial assembles working identity from the pieces enrollment returns: the
// node's own key, the DER leaf the CA signed for it, and the DER root to trust.
// It re-derives the bound name from the leaf so a node always reports the
// identity it actually holds rather than one it assumes.
func NewMaterial(key ed25519.PrivateKey, leafDER, rootDER []byte) (*Material, error) {
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		return nil, fmt.Errorf("service identity: parse leaf: %w", err)
	}
	name, err := nameFromCert(leaf)
	if err != nil {
		return nil, err
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		return nil, fmt.Errorf("service identity: parse root: %w", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)

	return &Material{
		cert:  tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: key, Leaf: leaf},
		name:  name,
		roots: pool,
	}, nil
}

// Name returns the identity this material asserts.
func (m *Material) Name() Name { return m.name }

// ServerTLSConfig builds the TLS configuration for a Connect endpoint that
// requires every caller to present a valid identity. Peer leaves are verified
// against the trust root; an unverifiable or absent client certificate fails the
// handshake before any request is dispatched.
func (m *Material) ServerTLSConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{m.cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    m.roots,
		MinVersion:   tls.VersionTLS13,
	}
}

// ClientTLSConfig builds the TLS configuration for dialing a peer. The node
// presents its own leaf and verifies the peer's chain against the trust root by
// identity rather than hostname: leaves carry URI SANs, not DNS names, so the
// default name check is replaced with explicit chain-and-identity verification.
func (m *Material) ClientTLSConfig() *tls.Config {
	return &tls.Config{
		Certificates:       []tls.Certificate{m.cert},
		InsecureSkipVerify: true, // chain is verified explicitly by identity below
		VerifyConnection:   verifyPeerIdentity(m.roots),
		MinVersion:         tls.VersionTLS13,
	}
}

// verifyPeerIdentity returns a connection verifier that checks the peer's leaf
// chains to the trust root and carries a well-formed service identity. It is the
// hostname-free equivalent of standard verification for SPIFFE-style URI SANs.
func verifyPeerIdentity(roots *x509.CertPool) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return fmt.Errorf("service identity: peer presented no certificate")
		}
		inter := x509.NewCertPool()
		for _, c := range cs.PeerCertificates[1:] {
			inter.AddCert(c)
		}
		if _, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{
			Roots:         roots,
			Intermediates: inter,
		}); err != nil {
			return fmt.Errorf("service identity: verify peer chain: %w", err)
		}
		_, err := nameFromCert(cs.PeerCertificates[0])
		return err
	}
}

// nameFromCert extracts the single service identity bound into a leaf's URI SAN,
// rejecting a certificate that carries none or more than one.
func nameFromCert(cert *x509.Certificate) (Name, error) {
	if len(cert.URIs) != 1 {
		return Name{}, fmt.Errorf("service identity: leaf must carry exactly one identity URI, got %d", len(cert.URIs))
	}
	return ParseName(cert.URIs[0])
}
