package registry

import (
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/registry"
)

// Membership is the node directory: it knows which storage nodes exist and how
// to dial them. In --mode=all there is exactly one node — the local process —
// so membership is a single fixed entry. Real discovery (health, joins, leaves)
// replaces this without changing how the Registry resolves an id to an address.
type Membership struct {
	local registry.Node
}

// NewMembership builds the single-node membership for the local process. The
// address is the loopback dial target peers use to reach this node's storage;
// it is empty/self when everything runs in one process.
func NewMembership(local registry.Node) *Membership {
	return &Membership{local: local}
}

// Local returns the node this process serves. New placements default to it.
func (m *Membership) Local() registry.Node {
	return m.local
}

// Lookup resolves a node id to its dial target. With a single node, only the
// local id is known; anything else is an unknown placement target.
func (m *Membership) Lookup(nodeID string) (registry.Node, error) {
	if nodeID == m.local.ID {
		return m.local, nil
	}
	return registry.Node{}, giterr.PlacementMiss("unknown storage node %q", nodeID)
}
