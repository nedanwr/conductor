package repostorage

import (
	"context"
	"errors"
	"io"

	"connectrpc.com/connect"

	"github.com/nedanwr/conductor/git-server/internal/core/repostorage"
	v1 "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/repostorage/v1"
	"github.com/nedanwr/conductor/git-server/internal/gen/gitserver/repostorage/v1/repostoragev1connect"
	"github.com/nedanwr/conductor/git-server/internal/transport"
)

// ConnectHandler adapts a core RepoStorage impl onto the generated Connect
// service so it can be served over the network. It translates the framed bidi
// streams into the io.Reader/io.Writer the impl expects and maps typed errors to
// Connect codes at the boundary. It serves any RepoStorage — in the slice, the
// in-process Store.
type ConnectHandler struct {
	repostoragev1connect.UnimplementedRepoStorageServiceHandler
	impl repostorage.RepoStorage
}

// NewConnectHandler wraps impl for serving. The returned handler implements the
// generated RepoStorageServiceHandler interface.
func NewConnectHandler(impl repostorage.RepoStorage) *ConnectHandler {
	return &ConnectHandler{impl: impl}
}

// endOfStream normalizes the client's half-close into a clean io.EOF. Connect
// surfaces end-of-stream as an error wrapping io.EOF; the pack programs need a
// real io.EOF on stdin to distinguish an orderly client flush from a broken
// connection, so we translate it here at the seam.
func endOfStream(err error) error {
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	return err
}

// CreateRepo serves the unary repo-init RPC.
func (h *ConnectHandler) CreateRepo(ctx context.Context, req *connect.Request[v1.CreateRepoRequest]) (*connect.Response[v1.CreateRepoResponse], error) {
	if err := h.impl.CreateRepo(ctx, req.Msg.GetRepoId(), req.Msg.GetDefaultBranch()); err != nil {
		return nil, transport.AsConnectError(err)
	}
	return connect.NewResponse(&v1.CreateRepoResponse{}), nil
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

// Receive serves receive-pack, mirroring Fetch's framing.
func (h *ConnectHandler) Receive(ctx context.Context, stream *connect.BidiStream[v1.ReceiveRequest, v1.ReceiveResponse]) error {
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
		if err := stream.Send(&v1.ReceiveResponse{Data: p}); err != nil {
			return 0, err
		}
		return len(p), nil
	})

	if err := h.impl.Receive(ctx, req, r, w); err != nil {
		return transport.AsConnectError(err)
	}
	return nil
}
