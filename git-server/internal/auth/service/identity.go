// Package service is the service-identity trust domain: who one of our processes
// is when it calls another. In --mode=all every service shares one process, so
// there is no peer to authenticate and the anchor admits the single implicit
// caller. In a split deployment each node enrolls with the cluster trust anchor,
// receives a short-lived signed identity, and proves it to peers over mTLS; the
// anchor then resolves a verified caller to its Name. Either way the call sites
// see only the Anchor interface and cannot tell which binding they received.
package service

import (
	"context"
	"fmt"
	"net/http"
)

// Identity is a resolved, authenticated service principal — the peer on the
// other end of a service-to-service call, as proven by its credential. In the
// co-located deployment there is one implicit identity with an empty Name; in a
// split deployment the Name is recovered from the peer's verified certificate.
type Identity struct {
	Name Name
}

// Anchor verifies the identity of a calling peer service. It is the callee's view
// of the service trust domain: given a request context, it either resolves the
// caller to an Identity or rejects it.
type Anchor interface {
	Verify(ctx context.Context) (Identity, error)
}

// NoopAnchor admits every caller with the empty Identity. It is the --mode=all
// anchor: one process, no peer, nothing to distinguish.
type NoopAnchor struct{}

// Verify always succeeds.
func (NoopAnchor) Verify(context.Context) (Identity, error) { return Identity{}, nil }

// PeerAnchor resolves the identity established by mTLS for the current request.
// The handshake has already proven the peer belongs to the deployment; this
// anchor recovers which peer it was so the callee can attribute and, if it
// chooses, reason about the caller's role. A request carrying no verified peer
// identity is rejected — a remote endpoint must never be reachable unauthenticated.
type PeerAnchor struct{}

// Verify returns the identity the mTLS middleware recorded for this request.
func (PeerAnchor) Verify(ctx context.Context) (Identity, error) {
	name, ok := peerFromContext(ctx)
	if !ok {
		return Identity{}, fmt.Errorf("service identity: no authenticated peer on request")
	}
	return Identity{Name: name}, nil
}

// peerKey is the context key under which the middleware stores the verified peer
// name. Unexported so only this package can set or read it.
type peerKey struct{}

// peerFromContext returns the verified peer name recorded for the request, if any.
func peerFromContext(ctx context.Context) (Name, bool) {
	name, ok := ctx.Value(peerKey{}).(Name)
	return name, ok
}

// ServerMiddleware authenticates the caller of an internal endpoint: it recovers
// the peer identity the mTLS handshake established, records it on the request
// context for the handler to attribute, and requires the anchor to admit it —
// rejecting any request that carries no verified identity before it reaches an
// RPC. It wraps every Connect endpoint a node exposes in a split deployment, so
// "authenticate the decider" holds at every service boundary without each handler
// re-implementing the check.
func ServerMiddleware(anchor Anchor, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			if name, err := nameFromCert(r.TLS.PeerCertificates[0]); err == nil {
				ctx = context.WithValue(ctx, peerKey{}, name)
			}
		}
		if _, err := anchor.Verify(ctx); err != nil {
			http.Error(w, "service identity required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
