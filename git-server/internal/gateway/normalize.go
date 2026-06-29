package gateway

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	"github.com/nedanwr/conductor/git-server/internal/db/queries"
)

// normalize builds the transport-agnostic boundary payload from an intake, the
// resolved repo UUID, and the authorization grant. This is the seam the spec
// guarantees: for the same logical operation the resulting GitRequest is
// byte-identical no matter which transport produced the intake, because nothing
// transport-specific is carried into it. A correlation id is minted here when the
// edge did not supply one, so every downstream call is traceable.
func normalize(in Intake, repoID uuid.UUID, grant gitreq.Grant) gitreq.GitRequest {
	corr := in.CorrelationID
	if corr == "" {
		corr = uuid.NewString()
	}
	return gitreq.GitRequest{
		RepoID:    repoID.String(),
		Operation: in.Operation,
		Principal: gitreq.Principal{
			UserID:    principalUserID(in.Principal),
			Anonymous: in.Principal.Anonymous,
		},
		Grant:         grant,
		Protocol:      in.Protocol,
		CorrelationID: corr,
	}
}

// principalUserID renders the carried user id, leaving it empty for the
// anonymous principal so attribution never invents an identity.
func principalUserID(u auth.User) string {
	if u.Anonymous || u.ID == uuid.Nil {
		return ""
	}
	return u.ID.String()
}

// OperationForService maps a git smart-protocol service name to the boundary
// operation. The two service names are the only valid git wire services; any
// other is rejected as unauthorized rather than guessed at. Both terminators use
// it so service-name parsing is shared, not reimplemented per transport.
func OperationForService(service string) (gitreq.Operation, error) {
	switch service {
	case "git-upload-pack":
		return gitreq.OperationFetch, nil
	case "git-receive-pack":
		return gitreq.OperationReceive, nil
	default:
		return gitreq.OperationUnspecified, giterr.Unauthorized("unsupported git service %q", service)
	}
}

// DBRepoResolver resolves owner/repo to the internal repo UUID against the repos
// table. It is the addressing seam: a single point translating the public name
// into the identity used for placement, storage, and the boundary payload.
type DBRepoResolver struct {
	q repoQuerier
}

// repoQuerier is the narrow slice of the generated query set the resolver needs.
// Narrowing it keeps the dependency explicit and lets tests substitute a fake.
type repoQuerier interface {
	GetRepoByOwnerName(ctx context.Context, arg queries.GetRepoByOwnerNameParams) (queries.Repo, error)
}

// NewDBRepoResolver builds a resolver over the given query set.
func NewDBRepoResolver(q repoQuerier) *DBRepoResolver {
	return &DBRepoResolver{q: q}
}

// Resolve returns the UUID of the repo addressed by owner/name. A missing repo
// is a typed RepoNotFound; the underlying query failure is wrapped as a DB error.
func (r *DBRepoResolver) Resolve(ctx context.Context, owner, name string) (uuid.UUID, error) {
	repo, err := r.q.GetRepoByOwnerName(ctx, queries.GetRepoByOwnerNameParams{Owner: owner, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, giterr.RepoNotFound("no repo %s/%s", owner, name)
	}
	if err != nil {
		return uuid.Nil, giterr.Wrap(giterr.KindDB, err, "lookup repo %s/%s", owner, name)
	}
	return repo.ID, nil
}
