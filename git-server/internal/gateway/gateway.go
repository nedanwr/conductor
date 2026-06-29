package gateway

import (
	"context"
	"io"

	"github.com/google/uuid"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	"github.com/nedanwr/conductor/git-server/internal/core/registry"
	"github.com/nedanwr/conductor/git-server/internal/core/repostorage"
)

// RepoResolver maps a user-facing owner/repo pair to the stable internal repo
// UUID. It is the one place the edge turns the addressable name into the
// identity every downstream tier speaks; past it, owner/repo is never re-parsed.
type RepoResolver interface {
	Resolve(ctx context.Context, owner, name string) (uuid.UUID, error)
}

// Authorizer decides whether a principal may perform an operation on a repo and
// returns the grant to carry downstream. It is satisfied by the sole authZ
// evaluator; the Gateway never makes an access decision of its own.
type Authorizer interface {
	Can(ctx context.Context, principal auth.User, repoID uuid.UUID, op gitreq.Operation) (gitreq.Grant, error)
}

// Router turns a placement node into the RepoStorage that owns it. In a
// co-located deployment it hands back the in-process store; split across hosts
// it hands back a Connect client adapter. Either way the Gateway holds only the
// interface and cannot tell which.
type Router interface {
	Route(node registry.Node) (repostorage.RepoStorage, error)
}

// Intake is the transport-neutral result of terminating a connection: who is
// asking, for which repo, to do what, under which negotiated protocol. Both the
// SSH and HTTPS edges produce one of these and hand it to the Gateway; nothing
// in it records which transport carried it.
type Intake struct {
	// Owner and Repo are the parsed user-facing address (owner/repo).
	Owner string
	Repo  string
	// Operation is the requested git operation.
	Operation gitreq.Operation
	// Principal is the already-authenticated identity (the anonymous principal
	// for an unauthenticated public fetch).
	Principal auth.User
	// Protocol is the negotiated git wire protocol and interaction shape.
	Protocol gitreq.ProtocolParams
	// CorrelationID threads tracing from the edge; the Gateway mints one when the
	// edge leaves it empty.
	CorrelationID string
}

// Gateway is the dual-transport edge's transport-agnostic core. The SSH and
// HTTPS terminators do their transport-specific authN and parsing, then converge
// here: resolve the repo identity, authorize once, normalize, and route to
// storage. Everything from this point is identical regardless of how the request
// arrived.
type Gateway struct {
	repos    RepoResolver
	authz    Authorizer
	registry registry.Registry
	router   Router
}

// New assembles a Gateway over the repo resolver, the authorizer, the placement
// registry, and the storage router.
func New(repos RepoResolver, authz Authorizer, reg registry.Registry, router Router) *Gateway {
	return &Gateway{repos: repos, authz: authz, registry: reg, router: router}
}

// Serve runs one already-terminated, already-authenticated request to
// completion: it resolves owner/repo to the internal UUID, makes the single
// authorization decision, normalizes the request, looks up placement, and routes
// the byte stream to storage. r carries the client→server bytes and w receives
// the server→client bytes; pktline framing is git's concern. Every error it
// returns is typed, so each edge can map it to its own status.
func (g *Gateway) Serve(ctx context.Context, in Intake, r io.Reader, w io.Writer) error {
	repoID, err := g.repos.Resolve(ctx, in.Owner, in.Repo)
	if err != nil {
		return err
	}

	grant, err := g.authz.Can(ctx, in.Principal, repoID, in.Operation)
	if err != nil {
		return err
	}

	req := normalize(in, repoID, grant)

	node, err := g.registry.ResolvePlacement(ctx, req.RepoID)
	if err != nil {
		return err
	}
	store, err := g.router.Route(node)
	if err != nil {
		return err
	}

	switch in.Operation {
	case gitreq.OperationFetch:
		return store.Fetch(ctx, req, r, w)
	case gitreq.OperationReceive:
		return store.Receive(ctx, req, r, w)
	default:
		return giterr.Unauthorized("unsupported git operation")
	}
}
