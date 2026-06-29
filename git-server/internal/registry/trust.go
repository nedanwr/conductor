package registry

import (
	"context"
	"crypto/subtle"
	"time"

	"github.com/nedanwr/conductor/git-server/internal/auth/service"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
)

// TrustAnchor is the cluster's root of trust for service-to-service calls: the
// authority that vouches for which peers belong to the deployment by signing
// their short-lived identities at enrollment. It is kept logically separate from
// the placement directory so "where everything is" and "the root of all trust"
// are not one compromise surface. It is consulted only at enrollment and
// rotation; steady-state peer auth is direct mTLS against the root it issues,
// never a call back here.
//
// In --mode=all there is one process and no peer to vouch for, so no anchor runs
// at all; the anchor exists for the split deployment, where each joining node
// presents a bootstrap secret and a CSR and receives a leaf it alone can wield.
type TrustAnchor struct {
	ca             *service.CA
	bootstrapToken string
	ttl            time.Duration
}

// Compile-time check that the anchor can serve the enrollment endpoint.
var _ service.Issuer = (*TrustAnchor)(nil)

// NewTrustAnchor builds the anchor over a certificate authority, the pre-shared
// bootstrap secret that gates who may enroll, and the lifetime of issued
// identities. A non-empty bootstrap token is required for any enrollment to
// succeed; an empty configured token refuses everyone rather than admitting all.
func NewTrustAnchor(ca *service.CA, bootstrapToken string, ttl time.Duration) *TrustAnchor {
	if ttl <= 0 {
		ttl = service.DefaultIdentityTTL
	}
	return &TrustAnchor{ca: ca, bootstrapToken: bootstrapToken, ttl: ttl}
}

// Issue validates the bootstrap secret and signs a short-lived leaf for the
// requested identity from the presented CSR. A missing or mismatched token is
// rejected as unauthorized before any signing happens; the comparison is
// constant-time so a wrong token leaks nothing through timing.
func (a *TrustAnchor) Issue(_ context.Context, p service.EnrollParams) (service.EnrollResult, error) {
	if !a.tokenOK(p.Token) {
		return service.EnrollResult{}, giterr.Unauthorized("enrollment: invalid bootstrap token")
	}
	leaf, notAfter, err := a.ca.IssueFromCSR(p.CSR, p.Name, a.ttl)
	if err != nil {
		return service.EnrollResult{}, giterr.Wrap(giterr.KindUnauthorized, err, "enrollment: issue identity")
	}
	return service.EnrollResult{LeafDER: leaf, RootDER: a.ca.RootDER(), NotAfter: notAfter}, nil
}

// tokenOK reports whether presented matches the configured bootstrap token,
// refusing when either side is empty.
func (a *TrustAnchor) tokenOK(presented string) bool {
	if a.bootstrapToken == "" || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a.bootstrapToken), []byte(presented)) == 1
}
