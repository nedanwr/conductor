package registry

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/db/queries"
)

// Querier is the subset of the generated query set the placement directory
// depends on. Narrowing it keeps the dependency explicit and lets tests
// substitute a fake without standing up Postgres.
type Querier interface {
	ResolvePlacement(ctx context.Context, repoID uuid.UUID) (queries.RepoPlacement, error)
	CreatePlacement(ctx context.Context, arg queries.CreatePlacementParams) (queries.RepoPlacement, error)
}

// Directory is the placement table data-access layer: repo UUID → storage node
// id. It returns the bare node id; resolving that id to a dialable address is
// the Membership's job, so the directory stays purely a mapping table.
type Directory struct {
	q Querier
}

// NewDirectory builds a Directory over the given query set.
func NewDirectory(q Querier) *Directory {
	return &Directory{q: q}
}

// Resolve returns the storage node id owning repoID. A repo with no placement
// row is a typed PlacementMiss; a malformed id is a RepoNotFound, since an
// unparseable UUID can never resolve.
func (d *Directory) Resolve(ctx context.Context, repoID string) (string, error) {
	id, err := uuid.Parse(repoID)
	if err != nil {
		return "", giterr.RepoNotFound("invalid repo id %q", repoID)
	}
	row, err := d.q.ResolvePlacement(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", giterr.PlacementMiss("no placement for repo %s", repoID)
	}
	if err != nil {
		return "", giterr.Wrap(giterr.KindDB, err, "resolve placement")
	}
	return row.StorageNodeID, nil
}

// Create records that repoID is placed on nodeID and returns the stored node id.
func (d *Directory) Create(ctx context.Context, repoID, nodeID string) (string, error) {
	id, err := uuid.Parse(repoID)
	if err != nil {
		return "", giterr.RepoNotFound("invalid repo id %q", repoID)
	}
	row, err := d.q.CreatePlacement(ctx, queries.CreatePlacementParams{
		RepoID:        id,
		StorageNodeID: nodeID,
	})
	if err != nil {
		return "", giterr.Wrap(giterr.KindDB, err, "create placement")
	}
	return row.StorageNodeID, nil
}
