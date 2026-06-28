package cache

import (
	"context"
	"io"

	"connectrpc.com/connect"

	"github.com/nedanwr/conductor/git-server/internal/core/cache"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	v1 "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/cache/v1"
	"github.com/nedanwr/conductor/git-server/internal/gen/gitserver/cache/v1/cachev1connect"
	"github.com/nedanwr/conductor/git-server/internal/transport"
)

// ConnectClient dials a remote CacheService and presents it as a core Cache. It
// is the remote half of the location-transparency seam: the wiring root can hand
// a consumer this client or the in-process passthrough, and the consumer behaves
// identically. Remote errors are re-raised as the same typed Kinds the impl
// would have returned.
type ConnectClient struct {
	client cachev1connect.CacheServiceClient
}

// Compile-time check that the client satisfies the core interface.
var _ cache.Cache = (*ConnectClient)(nil)

// NewConnectClient builds a ConnectClient against baseURL using httpClient.
func NewConnectClient(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) *ConnectClient {
	return &ConnectClient{
		client: cachev1connect.NewCacheServiceClient(httpClient, baseURL, opts...),
	}
}

// Fetch drives the remote upload-pack stream, shuttling client bytes from r up
// and pack bytes from the server down to w.
func (c *ConnectClient) Fetch(ctx context.Context, req gitreq.GitRequest, r io.Reader, w io.Writer) error {
	stream := c.client.Fetch(ctx)
	if err := stream.Send(&v1.FetchRequest{Payload: &v1.FetchRequest_Request{Request: toProtoGitRequest(req)}}); err != nil {
		return transport.FromConnectError(err)
	}

	sendErr := make(chan error, 1)
	go func() {
		sendErr <- pumpUp(r, func(b []byte) error {
			return stream.Send(&v1.FetchRequest{Payload: &v1.FetchRequest_Data{Data: b}})
		}, stream.CloseRequest)
	}()

	recvErr := pumpDown(w, func() ([]byte, error) {
		msg, err := stream.Receive()
		if err != nil {
			return nil, err
		}
		return msg.GetData(), nil
	})
	_ = stream.CloseResponse()

	return firstErr(recvErr, <-sendErr)
}
