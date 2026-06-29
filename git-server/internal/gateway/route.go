package gateway

import (
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/registry"
	"github.com/nedanwr/conductor/git-server/internal/core/repostorage"
)

// SingleRouter routes every placement to one storage backend. It is the
// co-located case: with a single storage node, placement resolves to that node
// and routing is a constant. It still goes through the Router seam, so swapping
// in a node-addressed router later changes wiring, not the Gateway.
type SingleRouter struct {
	node  registry.Node
	store repostorage.RepoStorage
}

// Compile-time check that SingleRouter satisfies the Router seam.
var _ Router = (*SingleRouter)(nil)

// NewSingleRouter binds the one storage backend and the node id it serves.
func NewSingleRouter(nodeID string, store repostorage.RepoStorage) *SingleRouter {
	return &SingleRouter{node: registry.Node{ID: nodeID}, store: store}
}

// Route returns the single backend, rejecting any node id other than the one it
// serves so a placement pointing elsewhere surfaces as a typed miss rather than
// silently hitting the wrong disk.
func (r *SingleRouter) Route(node registry.Node) (repostorage.RepoStorage, error) {
	if node.ID != r.node.ID {
		return nil, giterr.PlacementMiss("no storage backend for node %q", node.ID)
	}
	return r.store, nil
}
