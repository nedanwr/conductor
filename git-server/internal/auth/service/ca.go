package service

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net/url"
	"time"
)

// DefaultIdentityTTL is how long an issued service identity is valid. It is
// deliberately short: identities are cheap to reissue at enrollment and rotation,
// and a leaked credential expires on its own rather than being trusted forever.
const DefaultIdentityTTL = time.Hour

// CA is the cluster's trust anchor: the self-signed root that vouches for which
// peers belong to the deployment by signing their short-lived leaf identities.
// One CA exists per deployment, held by the enrollment authority; every node
// trusts its root and nothing else. The root key never leaves the authority —
// nodes receive only leaf certificates and the public root to verify against.
type CA struct {
	cert *x509.Certificate
	der  []byte
	key  ed25519.PrivateKey
}

// NewCA generates a fresh root: an ed25519 key and a self-signed CA certificate.
// A generated root is suitable for development and tests; a deployment that must
// survive restarts persists and reloads the root with LoadCA.
func NewCA() (*CA, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("service ca: generate root key: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: trustDomain + " service root"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		return nil, fmt.Errorf("service ca: self-sign root: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("service ca: parse root: %w", err)
	}
	return &CA{cert: cert, der: der, key: priv}, nil
}

// RootDER returns the DER-encoded root certificate, the bundle a node trusts to
// verify the peers it talks to.
func (c *CA) RootDER() []byte { return c.der }

// Pool returns a certificate pool containing only this root, for verifying peer
// chains against the deployment's single trust anchor.
func (c *CA) Pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(c.cert)
	return pool
}

// IssueFromCSR signs a leaf certificate for name, binding the identity into a
// URI SAN and using the public key from the presented certificate request. The
// requester proves possession of the corresponding private key by signing the
// CSR; the CA assigns the name from the validated enrollment, never trusting a
// name the requester put in the CSR subject. The leaf is short-lived per ttl.
func (c *CA) IssueFromCSR(csrDER []byte, name Name, ttl time.Duration) (leafDER []byte, notAfter time.Time, err error) {
	if !name.valid() {
		return nil, time.Time{}, fmt.Errorf("service ca: cannot issue for incomplete name")
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("service ca: parse csr: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, time.Time{}, fmt.Errorf("service ca: csr signature: %w", err)
	}
	return c.issue(csr.PublicKey, name, ttl)
}

// issue is the shared signing path: it builds and signs a leaf for the given
// public key and name. Kept private so the only ways to obtain a leaf are a CSR
// (the enrollment path) or the CA's own self-issue.
func (c *CA) issue(pub crypto.PublicKey, name Name, ttl time.Duration) ([]byte, time.Time, error) {
	notAfter := time.Now().Add(ttl)
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: name.String()},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		URIs:         []*url.URL{name.URI()},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, pub, c.key)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("service ca: sign leaf: %w", err)
	}
	return der, notAfter, nil
}

// serial draws a random certificate serial number. A collision is astronomically
// unlikely within a deployment's set of short-lived leaves.
func serial() *big.Int {
	max := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		// rand failure is unrecoverable; fall back to a time-based serial so cert
		// creation can still proceed rather than panicking in the crypto path.
		return big.NewInt(time.Now().UnixNano())
	}
	return n
}
