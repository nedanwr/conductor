package registry

import "context"

// TrustAnchor is the cluster's root of trust for service-to-service calls: the
// authority that vouches for which peers belong to the deployment. In --mode=all
// every service shares one process, so there is no peer to vouch for and the
// anchor admits all callers. It grows into an enrollment CA issuing short-lived
// service identities, replacing this no-op without touching the call sites.
type TrustAnchor interface {
	// Trusted reports whether the calling peer belongs to the deployment.
	Trusted(ctx context.Context) (bool, error)
}

// NoopTrustAnchor trusts every caller. It is the --mode=all anchor.
type NoopTrustAnchor struct{}

// Trusted always admits the caller.
func (NoopTrustAnchor) Trusted(context.Context) (bool, error) { return true, nil }

// NewTrustAnchor returns the anchor for the current deployment. Today that is
// always the no-op anchor; the constructor is the seam where an enrollment-backed
// CA will be selected.
func NewTrustAnchor() TrustAnchor { return NoopTrustAnchor{} }
