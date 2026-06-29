package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"connectrpc.com/connect"

	"github.com/nedanwr/conductor/git-server/internal/auth/service"
	"github.com/nedanwr/conductor/git-server/internal/transport"
)

// enrollDeadline bounds how long a starting node waits for the registry's
// enrollment endpoint to become reachable before giving up. A split fleet starts
// its processes independently, so a peer may come up before the registry; the
// node retries within this window rather than failing the race.
const enrollDeadline = 30 * time.Second

// serviceName is the identity a process of this mode and node asserts: its role
// is the runtime mode it plays, its instance the node id. This is the single
// place the deployment's role vocabulary is bound to the identity vocabulary.
func serviceName(cfg Config) service.Name {
	return service.Name{Role: string(cfg.Mode), NodeID: cfg.NodeID}
}

// enroll obtains this node's working identity from the registry's bootstrap
// endpoint, retrying until it answers or the deadline passes. It returns nil
// material when service identity is disabled, so callers transparently fall back
// to the plain transport — the same wiring serves both deployments.
func enroll(ctx context.Context, cfg Config) (*service.Material, error) {
	if !cfg.ServiceIdentity {
		return nil, nil
	}
	if cfg.EnrollURL == "" {
		return nil, fmt.Errorf("service identity enabled but no enrollment endpoint configured (set GITSERVER_ENROLL_URL)")
	}
	httpClient := transport.NewH2CClient()
	name := serviceName(cfg)

	deadline := time.Now().Add(enrollDeadline)
	for attempt := 1; ; attempt++ {
		mat, err := service.Enroll(ctx, httpClient, cfg.EnrollURL, cfg.BootstrapToken, name)
		if err == nil {
			slog.Info("enrolled service identity", "name", name.String())
			return mat, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("enroll %s: %w", name, err)
		}
		slog.Warn("enrollment not ready, retrying", "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// peerClient builds the HTTP client a node uses to reach its peers: mutually
// authenticated against the trust root when the node holds identity material,
// plain cleartext h2c otherwise.
func peerClient(mat *service.Material) connect.HTTPClient {
	if mat == nil {
		return transport.NewH2CClient()
	}
	return transport.NewMTLSClient(mat.ClientTLSConfig())
}
