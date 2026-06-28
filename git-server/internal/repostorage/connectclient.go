package repostorage

import (
	"context"
	"errors"
	"io"

	"connectrpc.com/connect"

	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	"github.com/nedanwr/conductor/git-server/internal/core/repostorage"
	v1 "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/repostorage/v1"
	"github.com/nedanwr/conductor/git-server/internal/gen/gitserver/repostorage/v1/repostoragev1connect"
	"github.com/nedanwr/conductor/git-server/internal/transport"
)

// streamChunk bounds how much client input is shuttled per frame.
const streamChunk = 32 * 1024

// ConnectClient dials a remote RepoStorageService and presents it as a core
// RepoStorage. It is the remote half of the location-transparency seam: the
// wiring root can hand a consumer this client or the in-process Store, and the
// consumer behaves identically. Remote errors are re-raised as the same typed
// Kinds the impl would have returned.
type ConnectClient struct {
	client repostoragev1connect.RepoStorageServiceClient
}

// Compile-time check that the client satisfies the core interface.
var _ repostorage.RepoStorage = (*ConnectClient)(nil)

// NewConnectClient builds a ConnectClient against baseURL using httpClient.
func NewConnectClient(httpClient connect.HTTPClient, baseURL string, opts ...connect.ClientOption) *ConnectClient {
	return &ConnectClient{
		client: repostoragev1connect.NewRepoStorageServiceClient(httpClient, baseURL, opts...),
	}
}

// CreateRepo calls the remote unary RPC.
func (c *ConnectClient) CreateRepo(ctx context.Context, repoID, defaultBranch string) error {
	_, err := c.client.CreateRepo(ctx, connect.NewRequest(&v1.CreateRepoRequest{
		RepoId:        repoID,
		DefaultBranch: defaultBranch,
	}))
	return transport.FromConnectError(err)
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

// Receive drives the remote receive-pack stream, mirroring Fetch.
func (c *ConnectClient) Receive(ctx context.Context, req gitreq.GitRequest, r io.Reader, w io.Writer) error {
	stream := c.client.Receive(ctx)
	if err := stream.Send(&v1.ReceiveRequest{Payload: &v1.ReceiveRequest_Request{Request: toProtoGitRequest(req)}}); err != nil {
		return transport.FromConnectError(err)
	}

	sendErr := make(chan error, 1)
	go func() {
		sendErr <- pumpUp(r, func(b []byte) error {
			return stream.Send(&v1.ReceiveRequest{Payload: &v1.ReceiveRequest_Data{Data: b}})
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

// pumpUp reads r in bounded chunks, sending each via send, then closes the
// request side. A short read is copied before sending so the buffer can be reused.
func pumpUp(r io.Reader, send func([]byte) error, closeRequest func() error) error {
	buf := make([]byte, streamChunk)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if serr := send(chunk); serr != nil {
				return transport.FromConnectError(serr)
			}
		}
		if errors.Is(err, io.EOF) {
			return closeRequest()
		}
		if err != nil {
			return err
		}
	}
}

// pumpDown writes server frames to w until the stream ends.
func pumpDown(w io.Writer, recv func() ([]byte, error)) error {
	for {
		data, err := recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return transport.FromConnectError(err)
		}
		if len(data) > 0 {
			if _, werr := w.Write(data); werr != nil {
				return werr
			}
		}
	}
}

// firstErr returns the first non-nil error, preferring the receive-side error
// since it carries the server's typed failure.
func firstErr(recvErr, sendErr error) error {
	if recvErr != nil {
		return recvErr
	}
	return sendErr
}
