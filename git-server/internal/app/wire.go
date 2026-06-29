package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/auth/user"
	connectcache "github.com/nedanwr/conductor/git-server/internal/cache"
	corecache "github.com/nedanwr/conductor/git-server/internal/core/cache"
	coreregistry "github.com/nedanwr/conductor/git-server/internal/core/registry"
	corestorage "github.com/nedanwr/conductor/git-server/internal/core/repostorage"
	"github.com/nedanwr/conductor/git-server/internal/db"
	"github.com/nedanwr/conductor/git-server/internal/gateway"
	"github.com/nedanwr/conductor/git-server/internal/gateway/https"
	gwssh "github.com/nedanwr/conductor/git-server/internal/gateway/ssh"
	cachev1connect "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/cache/v1/cachev1connect"
	registryv1connect "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/registry/v1/registryv1connect"
	repostoragev1connect "github.com/nedanwr/conductor/git-server/internal/gen/gitserver/repostorage/v1/repostoragev1connect"
	"github.com/nedanwr/conductor/git-server/internal/git"
	"github.com/nedanwr/conductor/git-server/internal/registry"
	"github.com/nedanwr/conductor/git-server/internal/repostorage"
	"github.com/nedanwr/conductor/git-server/internal/transport"
)

// runtime is the assembled, ready-to-run process: the servers the selected mode
// listens on and the resources to release on shutdown. It is the product of the
// wiring root and the only thing run.go needs to drive the lifecycle.
type runtime struct {
	servers []server
	closers []func()
}

// close releases held resources in reverse order of acquisition.
func (rt *runtime) close() {
	for i := len(rt.closers) - 1; i >= 0; i-- {
		rt.closers[i]()
	}
}

// wire constructs exactly the services the selected mode needs and binds each
// peer to either its in-process implementation (co-located) or a Connect client
// adapter (remote). This is the single place mode is interpreted; every package
// below it sees only interfaces and cannot tell which binding it received.
func wire(ctx context.Context, cfg Config) (*runtime, error) {
	rt := &runtime{}
	switch cfg.Mode {
	case ModeAll:
		return wireAll(ctx, cfg, rt)
	case ModeGateway:
		return wireGateway(ctx, cfg, rt)
	case ModeRepoStorage:
		return wireRepoStorage(cfg, rt)
	case ModeCache:
		return wireCache(cfg, rt)
	case ModeRegistry:
		return wireRegistry(ctx, cfg, rt)
	default:
		return nil, fmt.Errorf("cannot wire unknown mode %q", cfg.Mode)
	}
}

// wireAll co-locates every service in one process, connecting them with direct
// in-process calls. This is the single-binary deployment; the same edges and
// services run here that a split fleet runs, differing only in that the peers are
// reached by method call rather than over the network.
func wireAll(ctx context.Context, cfg Config, rt *runtime) (*runtime, error) {
	database, err := openDB(ctx, cfg, rt)
	if err != nil {
		return nil, err
	}

	store, err := newStore(cfg)
	if err != nil {
		return nil, err
	}

	reg := localRegistry(database, cfg)
	gw := newGateway(database, reg, gateway.NewSingleRouter(cfg.NodeID, store))

	if err := addEdges(cfg, rt, database, gw); err != nil {
		return nil, err
	}
	return rt, nil
}

// wireGateway runs only the edge, reaching the registry and storage as remote
// peers. The Gateway code is identical to the co-located case; only the bindings
// the wiring root injects differ.
func wireGateway(ctx context.Context, cfg Config, rt *runtime) (*runtime, error) {
	database, err := openDB(ctx, cfg, rt)
	if err != nil {
		return nil, err
	}

	httpClient := transport.NewH2CClient()
	var reg coreregistry.Registry = registry.NewConnectClient(httpClient, cfg.RegistryURL)
	var store corestorage.RepoStorage = repostorage.NewConnectClient(httpClient, cfg.RepoStorageURL)

	gw := newGateway(database, reg, gateway.NewSingleRouter(cfg.NodeID, store))
	if err := addEdges(cfg, rt, database, gw); err != nil {
		return nil, err
	}
	return rt, nil
}

// wireRepoStorage runs only the storage service, exposed over Connect for remote
// peers to reach.
func wireRepoStorage(cfg Config, rt *runtime) (*runtime, error) {
	store, err := newStore(cfg)
	if err != nil {
		return nil, err
	}
	path, handler := repostoragev1connect.NewRepoStorageServiceHandler(repostorage.NewConnectHandler(store))
	rt.servers = append(rt.servers, newConnectServer(cfg, path, handler))
	return rt, nil
}

// wireCache runs only the read-tier cache, passing through to a remote storage
// peer and exposing its own Connect endpoint.
func wireCache(cfg Config, rt *runtime) (*runtime, error) {
	httpClient := transport.NewH2CClient()
	var store corestorage.RepoStorage = repostorage.NewConnectClient(httpClient, cfg.RepoStorageURL)
	var c corecache.Cache = connectcache.New(store)
	path, handler := cachev1connect.NewCacheServiceHandler(connectcache.NewConnectHandler(c))
	rt.servers = append(rt.servers, newConnectServer(cfg, path, handler))
	return rt, nil
}

// wireRegistry runs only the placement registry, backed by Postgres and exposed
// over Connect.
func wireRegistry(ctx context.Context, cfg Config, rt *runtime) (*runtime, error) {
	database, err := openDB(ctx, cfg, rt)
	if err != nil {
		return nil, err
	}
	reg := localRegistry(database, cfg)
	path, handler := registryv1connect.NewRegistryServiceHandler(registry.NewConnectHandler(reg))
	rt.servers = append(rt.servers, newConnectServer(cfg, path, handler))
	return rt, nil
}

// newGateway assembles the transport-agnostic edge core over the database-backed
// resolver and authorizer, the placement registry, and the storage router.
func newGateway(database *db.DB, reg coreregistry.Registry, router gateway.Router) *gateway.Gateway {
	q := database.Queries()
	resolver := gateway.NewDBRepoResolver(q)
	authz := user.NewAuthorizer(auth.NewStore(q))
	return gateway.New(resolver, authz, reg, router)
}

// addEdges builds the SSH and HTTPS git terminators over the gateway core and
// registers them as servers. Both edges share the one authN store, so the same
// user identity and authZ source of truth back every transport.
func addEdges(cfg Config, rt *runtime, database *db.DB, gw *gateway.Gateway) error {
	authStore := auth.NewStore(database.Queries())
	authn := user.NewAuthenticator(authStore)

	signer, err := loadHostKey(cfg)
	if err != nil {
		return err
	}

	rt.servers = append(rt.servers,
		newHTTPSEdge(cfg, https.NewHandler(gw, authn)),
		newSSHEdge(cfg, gwssh.NewServer(gw, authn, signer)),
	)
	return nil
}

// localRegistry builds the in-process placement registry: the Postgres-backed
// directory composed with single-node membership for this process.
func localRegistry(database *db.DB, cfg Config) *registry.Registry {
	dir := registry.NewDirectory(database.Queries())
	mem := registry.NewMembership(coreregistry.Node{ID: cfg.NodeID, Address: cfg.ConnectAddr})
	return registry.New(dir, mem)
}

// newStore builds the on-disk storage implementation over the configured storage
// root and the system git binary.
func newStore(cfg Config) (*repostorage.Store, error) {
	runner, err := git.NewRunner()
	if err != nil {
		return nil, err
	}
	return repostorage.NewStore(cfg.StorageRoot, runner), nil
}

// openDB opens the Postgres pool, applies pending migrations, and registers the
// pool for release at shutdown. A missing DSN is a configuration error.
func openDB(ctx context.Context, cfg Config, rt *runtime) (*db.DB, error) {
	if cfg.DatabaseDSN == "" {
		return nil, fmt.Errorf("database DSN is required for mode %q (set DATABASE_URL)", cfg.Mode)
	}
	if err := db.MigrateUp(ctx, cfg.DatabaseDSN); err != nil {
		return nil, err
	}
	database, err := db.Open(ctx, cfg.DatabaseDSN)
	if err != nil {
		return nil, err
	}
	rt.closers = append(rt.closers, database.Close)
	return database, nil
}

// connectMux mounts a single Connect service path on a fresh mux wrapped for h2c
// so streaming RPCs work over cleartext HTTP/2.
func connectMux(path string, handler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	return transport.H2CHandler(mux)
}
