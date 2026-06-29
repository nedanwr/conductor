package https

import (
	"fmt"
	"net/http"
)

// serviceHeader builds the smart-HTTP advertisement preamble: a pkt-line naming
// the service followed by a flush packet, which precedes the ref advertisement
// the client expects from a GET info/refs response.
func serviceHeader(service string) []byte {
	payload := fmt.Sprintf("# service=%s\n", service)
	return append(pktLine(payload), flushPkt...)
}

// flushPkt is git's pkt-line flush marker.
var flushPkt = []byte("0000")

// pktLine encodes s as a single git pkt-line: a four-hex-digit length prefix
// (covering the prefix itself) followed by the payload.
func pktLine(s string) []byte {
	return []byte(fmt.Sprintf("%04x%s", len(s)+4, s))
}

// deferredWriter delays the response status and headers until the first body
// byte, so the Gateway's authorization and resolution errors — which all occur
// before any storage output — can still set an HTTP status. onFirst writes the
// status, headers, and any protocol preamble exactly once; until it fires the
// caller is free to send an error status instead.
type deferredWriter struct {
	w       http.ResponseWriter
	onFirst func(http.ResponseWriter) error
	started bool
}

// Write commits the headers (via onFirst) on the first call, then streams the
// body through to the underlying writer.
func (d *deferredWriter) Write(p []byte) (int, error) {
	if !d.started {
		d.started = true
		if err := d.onFirst(d.w); err != nil {
			return 0, err
		}
	}
	return d.w.Write(p)
}
