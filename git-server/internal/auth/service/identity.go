// Package service is the service-identity seam: no-op in --mode=all; enrollment
// and mTLS arrive later. It exists so peer-to-peer calls have an identity
// anchor to consult from the start, even while that anchor trusts everyone.
package service

import "context"

// Identity is a resolved service principal. In the co-located deployment there
// is exactly one implicit identity and nothing to distinguish, so the struct is
// intentionally empty; it grows a name and verified credentials with enrollment.
type Identity struct{}

// Anchor verifies the identity of a calling peer service. The slice runs every
// service in one process, so there is no peer to authenticate and the anchor
// admits all callers. A real anchor (mTLS / enrollment) replaces this without
// changing the call sites.
type Anchor interface {
	// Verify resolves the calling peer to a service Identity, or rejects it.
	Verify(ctx context.Context) (Identity, error)
}

// NoopAnchor admits every caller with the empty Identity. It is the --mode=all
// anchor.
type NoopAnchor struct{}

// Verify always succeeds.
func (NoopAnchor) Verify(context.Context) (Identity, error) { return Identity{}, nil }

// NewAnchor returns the anchor for the current deployment. Today that is always
// the no-op anchor; the constructor is the seam where enrollment-backed anchors
// will be selected.
func NewAnchor() Anchor { return NoopAnchor{} }
