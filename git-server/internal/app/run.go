package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nedanwr/conductor/git-server/internal/auth/service"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	gwssh "github.com/nedanwr/conductor/git-server/internal/gateway/ssh"
)

// shutdownTimeout bounds how long graceful shutdown waits for in-flight work to
// drain before listeners are forced closed.
const shutdownTimeout = 15 * time.Second

// server is one listening surface of a running process. The wiring root produces
// a set of these for the selected mode; Run drives their shared lifecycle without
// knowing whether a server is a git edge or an internal Connect endpoint.
type server interface {
	serve() error
	shutdown(ctx context.Context) error
	name() string
}

// Run assembles the process for cfg, starts every server the selected mode
// listens on, and blocks until a termination signal or a fatal server error.
// Shutdown is graceful: listeners stop accepting, in-flight work drains, and held
// resources (the Postgres pool) are released.
func Run(ctx context.Context, cfg Config) error {
	setupLogging(cfg)

	rt, err := wire(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.close()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, len(rt.servers))
	for _, s := range rt.servers {
		s := s
		slog.Info("listening", "server", s.name(), "mode", string(cfg.Mode))
		go func() { errCh <- s.serve() }()
	}

	var runErr error
	select {
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	case runErr = <-errCh:
		if runErr != nil {
			slog.Error("server failed", "error", runErr)
		}
	}

	shutdownAll(rt.servers)
	return runErr
}

// shutdownAll stops every server within the shutdown deadline.
func shutdownAll(servers []server) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, s := range servers {
		s := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.shutdown(ctx); err != nil {
				slog.Error("shutdown", "server", s.name(), "error", err)
			}
		}()
	}
	wg.Wait()
}

// setupLogging configures the default slog level from the resolved config.
func setupLogging(cfg Config) {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetLogLoggerLevel(level)
}

// httpEdge is a server backed by an http.Server: the HTTPS git edge or an
// internal Connect endpoint. When cert and key paths are set it serves TLS;
// otherwise it serves plain HTTP, acceptable only for development.
type httpEdge struct {
	label    string
	srv      *http.Server
	certPath string
	keyPath  string
}

func (h *httpEdge) name() string { return h.label }

func (h *httpEdge) serve() error {
	var err error
	switch {
	case h.srv.TLSConfig != nil:
		// mTLS connect endpoint: certificates and client-auth policy come from the
		// configured TLSConfig, so no cert/key paths are passed.
		err = h.srv.ListenAndServeTLS("", "")
	case h.certPath != "" && h.keyPath != "":
		err = h.srv.ListenAndServeTLS(h.certPath, h.keyPath)
	default:
		err = h.srv.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (h *httpEdge) shutdown(ctx context.Context) error { return h.srv.Shutdown(ctx) }

// newHTTPSEdge builds the smart-HTTP git edge listener over handler.
func newHTTPSEdge(cfg Config, handler http.Handler) server {
	return &httpEdge{
		label:    "https",
		srv:      &http.Server{Addr: cfg.HTTPSAddr, Handler: handler},
		certPath: cfg.TLSCertPath,
		keyPath:  cfg.TLSKeyPath,
	}
}

// newConnectServer builds an internal Connect endpoint serving a single service
// path on the configured Connect address. With identity material it serves mTLS:
// every caller must present a verified service identity, recovered onto the
// request before the handler runs. Without material it serves cleartext h2c, the
// development and single-binary path where there is no peer to authenticate.
func newConnectServer(cfg Config, path string, handler http.Handler, mat *service.Material) server {
	if mat == nil {
		return &httpEdge{
			label: "connect",
			srv:   &http.Server{Addr: cfg.ConnectAddr, Handler: connectMux(path, handler)},
		}
	}
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	secured := service.ServerMiddleware(service.PeerAnchor{}, mux)
	return &httpEdge{
		label: "connect",
		srv: &http.Server{
			Addr:      cfg.ConnectAddr,
			Handler:   secured,
			TLSConfig: mat.ServerTLSConfig(),
		},
	}
}

// newEnrollServer builds the bootstrap enrollment endpoint on the registry's
// enrollment address. It is deliberately cleartext h2c and outside mTLS: a node
// enrolling here has no identity yet to present. Access is gated by the bootstrap
// token the enrollment handler checks, not by the transport.
func newEnrollServer(cfg Config, path string, handler http.Handler) server {
	return &httpEdge{
		label: "enroll",
		srv:   &http.Server{Addr: cfg.EnrollAddr, Handler: connectMux(path, handler)},
	}
}

// sshEdge is the SSH git edge listener. It owns its TCP listener so shutdown can
// stop accepts; the server's per-connection goroutines drain on their own once
// the listener is closed.
type sshEdge struct {
	addr string
	srv  *gwssh.Server

	mu     sync.Mutex
	ln     net.Listener
	closed bool
}

func (s *sshEdge) name() string { return "ssh" }

func (s *sshEdge) serve() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ln.Close()
	}
	s.ln = ln
	s.mu.Unlock()

	err = s.srv.Serve(ln)
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		// The accept loop ended because shutdown closed the listener.
		return nil
	}
	return err
}

func (s *sshEdge) shutdown(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.ln != nil {
		return s.ln.Close()
	}
	return nil
}

// newSSHEdge builds the SSH git edge listener over the SSH terminator.
func newSSHEdge(cfg Config, srv *gwssh.Server) server {
	return &sshEdge{addr: cfg.SSHAddr, srv: srv}
}

// loadHostKey returns the SSH host key the edge presents to clients. A configured
// PEM file is loaded; with no path an ephemeral key is generated, acceptable only
// for development since clients see a changed host key across restarts.
func loadHostKey(cfg Config) (ssh.Signer, error) {
	if cfg.SSHHostKeyPath != "" {
		pem, err := os.ReadFile(cfg.SSHHostKeyPath)
		if err != nil {
			return nil, giterr.Wrap(giterr.KindGitExec, err, "read ssh host key")
		}
		signer, err := ssh.ParsePrivateKey(pem)
		if err != nil {
			return nil, giterr.Wrap(giterr.KindGitExec, err, "parse ssh host key")
		}
		return signer, nil
	}

	slog.Warn("no SSH host key configured; generating an ephemeral key (development only)")
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, giterr.Wrap(giterr.KindGitExec, err, "generate ephemeral host key")
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, giterr.Wrap(giterr.KindGitExec, err, "build ephemeral signer")
	}
	return signer, nil
}
