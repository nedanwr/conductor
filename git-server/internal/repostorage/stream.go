package repostorage

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
