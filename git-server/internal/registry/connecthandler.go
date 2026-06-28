package registry

import (
	"context"

	"connectrpc.com/connect"

	"github.com/nedanwr/conductor/git-server/internal/core/registry"
	v1 "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/registry/v1"
	"github.com/nedanwr/conductor/git-server/internal/gen/gitserver/registry/v1/registryv1connect"
	"github.com/nedanwr/conductor/git-server/internal/transport"
)

// ConnectHandler adapts a core Registry impl onto the generated Connect service
// so it can be served over the network, mapping typed errors to Connect codes at
// the boundary. It serves any Registry — in the slice, the in-process impl.
type ConnectHandler struct {
	registryv1connect.UnimplementedRegistryServiceHandler
	impl registry.Registry
}

// NewConnectHandler wraps impl for serving. The returned handler implements the
// generated RegistryServiceHandler interface.
func NewConnectHandler(impl registry.Registry) *ConnectHandler {
	return &ConnectHandler{impl: impl}
}

// ResolvePlacement serves the unary resolve RPC.
func (h *ConnectHandler) ResolvePlacement(ctx context.Context, req *connect.Request[v1.ResolvePlacementRequest]) (*connect.Response[v1.ResolvePlacementResponse], error) {
	node, err := h.impl.ResolvePlacement(ctx, req.Msg.GetRepoId())
	if err != nil {
		return nil, transport.AsConnectError(err)
	}
	return connect.NewResponse(&v1.ResolvePlacementResponse{Node: toProtoNode(node)}), nil
}

// CreatePlacement serves the unary create RPC.
func (h *ConnectHandler) CreatePlacement(ctx context.Context, req *connect.Request[v1.CreatePlacementRequest]) (*connect.Response[v1.CreatePlacementResponse], error) {
	node, err := h.impl.CreatePlacement(ctx, req.Msg.GetRepoId(), req.Msg.GetNodeId())
	if err != nil {
		return nil, transport.AsConnectError(err)
	}
	return connect.NewResponse(&v1.CreatePlacementResponse{Node: toProtoNode(node)}), nil
}

// toProtoNode serializes a core Node onto the wire type.
func toProtoNode(n registry.Node) *v1.Node {
	return &v1.Node{Id: n.ID, Address: n.Address}
}

// fromProtoNode deserializes a wire Node back into the core type.
func fromProtoNode(n *v1.Node) registry.Node {
	if n == nil {
		return registry.Node{}
	}
	return registry.Node{ID: n.GetId(), Address: n.GetAddress()}
}
