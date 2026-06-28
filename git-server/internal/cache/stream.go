package cache

import (
	"errors"
	"io"

	"github.com/nedanwr/conductor/git-server/internal/transport"
)

// streamChunk bounds how much client input is shuttled per frame.
const streamChunk = 32 * 1024

// recvReader adapts a stream's frame-receive function into an io.Reader. Each
// call to next returns the raw bytes of one frame (empty frames — e.g. the
// metadata frame carrying no data — are skipped) and io.EOF ends the stream.
type recvReader struct {
	next func() ([]byte, error)
	rem  []byte
}

func (r *recvReader) Read(p []byte) (int, error) {
	for len(r.rem) == 0 {
		b, err := r.next()
		if err != nil {
			return 0, err
		}
		r.rem = b
	}
	n := copy(p, r.rem)
	r.rem = r.rem[n:]
	return n, nil
}

// writerFunc adapts a send function into an io.Writer so impl output can be
// framed back onto a stream.
type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

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

// endOfStream normalizes the client's half-close into a clean io.EOF, matching
// the repostorage seam so the pack programs see an orderly flush rather than a
// wrapped stream error.
func endOfStream(err error) error {
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	return err
}
