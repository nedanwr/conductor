package cache

import (
	"context"

	"connectrpc.com/connect"

	"github.com/nedanwr/conductor/git-server/internal/core/cache"
	v1 "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/cache/v1"
	"github.com/nedanwr/conductor/git-server/internal/gen/gitserver/cache/v1/cachev1connect"
	"github.com/nedanwr/conductor/git-server/internal/transport"
)

// ConnectHandler adapts a core Cache impl onto the generated Connect service so
// it can be served over the network. It translates the framed bidi stream into
// the io.Reader/io.Writer the impl expects and maps typed errors to Connect
// codes at the boundary, mirroring the repostorage Fetch seam.
type ConnectHandler struct {
	cachev1connect.UnimplementedCacheServiceHandler
	impl cache.Cache
}

// NewConnectHandler wraps impl for serving. The returned handler implements the
// generated CacheServiceHandler interface.
func NewConnectHandler(impl cache.Cache) *ConnectHandler {
	return &ConnectHandler{impl: impl}
}

// Fetch serves upload-pack. The first frame carries the GitRequest; the rest are
// raw client bytes shuttled to the impl, whose output is framed back to the
// caller.
func (h *ConnectHandler) Fetch(ctx context.Context, stream *connect.BidiStream[v1.FetchRequest, v1.FetchResponse]) error {
	first, err := stream.Receive()
	if err != nil {
		return transport.AsConnectError(err)
	}
	req := fromProtoGitRequest(first.GetRequest())

	r := &recvReader{next: func() ([]byte, error) {
		msg, err := stream.Receive()
		if err != nil {
			return nil, endOfStream(err)
		}
		return msg.GetData(), nil
	}}
	w := writerFunc(func(p []byte) (int, error) {
		if err := stream.Send(&v1.FetchResponse{Data: p}); err != nil {
			return 0, err
		}
		return len(p), nil
	})

	if err := h.impl.Fetch(ctx, req, r, w); err != nil {
		return transport.AsConnectError(err)
	}
	return nil
}
