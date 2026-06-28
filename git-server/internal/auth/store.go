package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/db/queries"
)

// Querier is the subset of the generated query set the auth concern depends on.
// Narrowing it keeps the dependency explicit and lets tests substitute a fake.
type Querier interface {
	GetSSHKeyByFingerprint(ctx context.Context, fingerprint string) (queries.SshKey, error)
	GetAccessTokenByHash(ctx context.Context, tokenHash string) (queries.AccessToken, error)
	GetUser(ctx context.Context, id uuid.UUID) (queries.User, error)
	GetRepo(ctx context.Context, id uuid.UUID) (queries.Repo, error)
	GetPermission(ctx context.Context, arg queries.GetPermissionParams) (queries.RepoPermission, error)
}

// Store is the data-access layer for the auth tables. It translates raw query
// results into auth domain types and into typed boundary errors.
type Store struct {
	q Querier
}

// NewStore builds a Store over the given query set.
func NewStore(q Querier) *Store {
	return &Store{q: q}
}

// UserByFingerprint resolves the user owning the SSH key with this fingerprint.
// A missing key is reported as Unauthorized, not RepoNotFound: an unknown key is
// an authentication failure.
func (s *Store) UserByFingerprint(ctx context.Context, fingerprint string) (User, error) {
	key, err := s.q.GetSSHKeyByFingerprint(ctx, fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, giterr.Unauthorized("unknown ssh key")
	}
	if err != nil {
		return User{}, giterr.Wrap(giterr.KindDB, err, "lookup ssh key")
	}
	return s.userByID(ctx, key.UserID)
}

// UserByTokenHash resolves the user owning the access token with this hash,
// rejecting an expired token. The caller hashes the presented token first; the
// raw token is never passed here.
func (s *Store) UserByTokenHash(ctx context.Context, tokenHash string) (User, error) {
	tok, err := s.q.GetAccessTokenByHash(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, giterr.Unauthorized("unknown access token")
	}
	if err != nil {
		return User{}, giterr.Wrap(giterr.KindDB, err, "lookup access token")
	}
	if tok.ExpiresAt.Valid && tok.ExpiresAt.Time.Before(now()) {
		return User{}, giterr.Unauthorized("access token expired")
	}
	return s.userByID(ctx, tok.UserID)
}

func (s *Store) userByID(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := s.q.GetUser(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		// The owning user vanished out from under the credential.
		return User{}, giterr.Unauthorized("user not found")
	}
	if err != nil {
		return User{}, giterr.Wrap(giterr.KindDB, err, "lookup user")
	}
	return User{ID: u.ID, Username: u.Username}, nil
}

// Visibility resolves a repo's visibility. A missing repo is RepoNotFound.
func (s *Store) Visibility(ctx context.Context, repoID uuid.UUID) (Visibility, error) {
	repo, err := s.q.GetRepo(ctx, repoID)
	if errors.Is(err, pgx.ErrNoRows) {
		return VisibilityPrivate, giterr.RepoNotFound("repo %s", repoID)
	}
	if err != nil {
		return VisibilityPrivate, giterr.Wrap(giterr.KindDB, err, "lookup repo")
	}
	return ParseVisibility(repo.Visibility)
}

// LevelFor resolves a user's permission level on a repo. The absence of a
// permission row is LevelNone (not an error): authorization decides what that
// means for the requested operation.
func (s *Store) LevelFor(ctx context.Context, userID, repoID uuid.UUID) (Level, error) {
	perm, err := s.q.GetPermission(ctx, queries.GetPermissionParams{
		UserID: userID,
		RepoID: repoID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return LevelNone, nil
	}
	if err != nil {
		return LevelNone, giterr.Wrap(giterr.KindDB, err, "lookup permission")
	}
	return ParseLevel(perm.Level)
}
