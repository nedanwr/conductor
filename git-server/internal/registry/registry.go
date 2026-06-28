package registry

import (
	"context"

	"github.com/nedanwr/conductor/git-server/internal/core/registry"
)

// Registry is the in-process control-plane directory. It composes the placement
// table with node membership to answer the core registry.Registry contract:
// resolve a repo to a dialable storage node, and record new placements. The
// wiring root binds either this impl or a Connect client adapter behind that
// interface, and no consumer can tell which.
type Registry struct {
	directory *Directory
	members   *Membership
}

// Compile-time check that Registry satisfies the core interface.
var _ registry.Registry = (*Registry)(nil)

// New assembles a Registry over a placement directory and node membership.
func New(directory *Directory, members *Membership) *Registry {
	return &Registry{directory: directory, members: members}
}

// ResolvePlacement returns the storage node owning repoID. The directory yields
// the node id; membership turns that id into a dialable address. In --mode=all
// every repo resolves to the single local node.
func (r *Registry) ResolvePlacement(ctx context.Context, repoID string) (registry.Node, error) {
	nodeID, err := r.directory.Resolve(ctx, repoID)
	if err != nil {
		return registry.Node{}, err
	}
	return r.members.Lookup(nodeID)
}

// CreatePlacement records that repoID is placed on nodeID and returns the
// resulting node. An empty nodeID places the repo on the local node, which is
// the only choice in --mode=all.
func (r *Registry) CreatePlacement(ctx context.Context, repoID, nodeID string) (registry.Node, error) {
	if nodeID == "" {
		nodeID = r.members.Local().ID
	}
	stored, err := r.directory.Create(ctx, repoID, nodeID)
	if err != nil {
		return registry.Node{}, err
	}
	return r.members.Lookup(stored)
}
