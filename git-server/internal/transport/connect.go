package transport

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// H2CHandler wraps a Connect handler so it speaks cleartext HTTP/2 (h2c) in
// addition to HTTP/1.1. Connect's streaming RPCs (the pack protocol's bidi
// streams) need HTTP/2; serving h2c lets split-deployment peers talk over plain
// loopback without TLS, while TLS deployments negotiate HTTP/2 the usual way.
func H2CHandler(h http.Handler) http.Handler {
	return h2c.NewHandler(h, &http2.Server{})
}

// NewH2CClient builds an HTTP client that dials peers over cleartext HTTP/2. It
// is the client half of the h2c seam used to reach remote services in a split
// deployment without service identity (development, and the bootstrap enrollment
// call a node makes before it has an identity to present).
func NewH2CClient() connect.HTTPClient {
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
}

// NewMTLSClient builds an HTTP/2 client that dials peers over TLS, presenting and
// verifying service identity per tlsConf. It is the client half of the mTLS seam:
// once a node holds enrolled Material, every call it makes to a peer goes through
// a client built here, so the peer can authenticate the caller and the caller can
// authenticate the peer against the shared trust root.
func NewMTLSClient(tlsConf *tls.Config) connect.HTTPClient {
	return &http.Client{
		Transport: &http2.Transport{TLSClientConfig: tlsConf},
	}
}
