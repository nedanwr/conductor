// Package registry defines the Registry service interface and its serializable
// boundary types. Interface only, no impl.
package registry

import "context"

// Node identifies a storage node in the placement directory. Address is the dial
// target for remote modes; in --mode=all it points at the single in-process node.
type Node struct {
	ID      string
	Address string
}

// Registry is the placement directory: repo UUID → storage node. It is off the
// per-request path — the Gateway caches placement and consults the Registry only
// on cache miss. Membership/discovery and the service trust anchor are trivial
// single-node seams in the slice and are not on this network interface (see
// internal/registry).
type Registry interface {
	// ResolvePlacement returns the storage node owning repoID.
	ResolvePlacement(ctx context.Context, repoID string) (Node, error)

	// CreatePlacement records that repoID is placed on nodeID and returns the
	// resulting node.
	CreatePlacement(ctx context.Context, repoID, nodeID string) (Node, error)
}
