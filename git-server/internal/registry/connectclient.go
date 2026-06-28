package registry

import (
	"context"

	"connectrpc.com/connect"

	"github.com/nedanwr/conductor/git-server/internal/core/registry"
	v1 "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/registry/v1"
	"github.com/nedanwr/conductor/git-server/internal/gen/gitserver/registry/v1/registryv1connect"
	"github.com/nedanwr/conductor/git-server/internal/transport"
)

// ConnectClient dials a remote RegistryService and presents it as a core
// Registry. It is the remote half of the location-transparency seam: the wiring
// root can hand a consumer this client or the in-process impl, and the consumer
// behaves identically. Remote errors are re-raised as the same typed Kinds the
// impl would have returned.
type ConnectClient struct {
	client registryv1connect.RegistryServiceClient
}

// Compile-time check that the client satisfies the core interface.
var _ registry.Registry = (*ConnectClient)(nil)

// NewConnectClient builds a ConnectClient against baseURL using httpClient.
func NewConnectClient(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) *ConnectClient {
	return &ConnectClient{
		client: registryv1connect.NewRegistryServiceClient(httpClient, baseURL, opts...),
	}
}

// ResolvePlacement calls the remote resolve RPC.
func (c *ConnectClient) ResolvePlacement(ctx context.Context, repoID string) (registry.Node, error) {
	resp, err := c.client.ResolvePlacement(ctx, connect.NewRequest(&v1.ResolvePlacementRequest{RepoId: repoID}))
	if err != nil {
		return registry.Node{}, transport.FromConnectError(err)
	}
	return fromProtoNode(resp.Msg.GetNode()), nil
}

// CreatePlacement calls the remote create RPC.
func (c *ConnectClient) CreatePlacement(ctx context.Context, repoID, nodeID string) (registry.Node, error) {
	resp, err := c.client.CreatePlacement(ctx, connect.NewRequest(&v1.CreatePlacementRequest{
		RepoId: repoID,
		NodeId: nodeID,
	}))
	if err != nil {
		return registry.Node{}, transport.FromConnectError(err)
	}
	return fromProtoNode(resp.Msg.GetNode()), nil
}
