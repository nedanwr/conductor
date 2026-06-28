package db_test

import (
	"context"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nedanwr/conductor/git-server/internal/db"
	"github.com/nedanwr/conductor/git-server/internal/db/queries"
)

// testDSN returns the Postgres DSN for integration tests, or skips the test when
// no database is configured (e.g. unit-only CI without compose).
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres round-trip test")
	}
	return dsn
}

func setup(t *testing.T) (*db.DB, context.Context) {
	t.Helper()
	dsn := testDSN(t)
	ctx := context.Background()

	if err := db.MigrateUp(ctx, dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	d, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(d.Close)
	return d, ctx
}

func TestUserRoundTrip(t *testing.T) {
	d, ctx := setup(t)
	q := d.Queries()

	username := "user_" + uniq()
	u, err := q.CreateUser(ctx, username)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteUser(context.Background(), u.ID) })

	got, err := q.GetUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if got.Username != username {
		t.Fatalf("username = %q, want %q", got.Username, username)
	}

	byName, err := q.GetUserByUsername(ctx, username)
	if err != nil {
		t.Fatalf("get user by username: %v", err)
	}
	if byName.ID != u.ID {
		t.Fatalf("id = %v, want %v", byName.ID, u.ID)
	}
}

func TestSSHKeyAndTokenRoundTrip(t *testing.T) {
	d, ctx := setup(t)
	q := d.Queries()

	u, err := q.CreateUser(ctx, "user_"+uniq())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteUser(context.Background(), u.ID) })

	fp := "SHA256:" + uniq()
	key, err := q.CreateSSHKey(ctx, queries.CreateSSHKeyParams{
		UserID:      u.ID,
		PublicKey:   "ssh-ed25519 AAAA...",
		Fingerprint: fp,
	})
	if err != nil {
		t.Fatalf("create ssh key: %v", err)
	}
	gotKey, err := q.GetSSHKeyByFingerprint(ctx, fp)
	if err != nil {
		t.Fatalf("get ssh key: %v", err)
	}
	if gotKey.ID != key.ID || gotKey.UserID != u.ID {
		t.Fatalf("ssh key mismatch: %+v", gotKey)
	}

	exp := pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}
	tok, err := q.CreateAccessToken(ctx, queries.CreateAccessTokenParams{
		UserID:    u.ID,
		TokenHash: "hash_" + uniq(),
		ExpiresAt: exp,
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	gotTok, err := q.GetAccessTokenByHash(ctx, tok.TokenHash)
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if gotTok.ID != tok.ID {
		t.Fatalf("token id = %v, want %v", gotTok.ID, tok.ID)
	}
}

func TestRepoPermissionPlacementRoundTrip(t *testing.T) {
	d, ctx := setup(t)
	q := d.Queries()

	u, err := q.CreateUser(ctx, "user_"+uniq())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteUser(context.Background(), u.ID) })

	repo, err := q.CreateRepo(ctx, queries.CreateRepoParams{
		Owner:         "owner_" + uniq(),
		Name:          "repo",
		Visibility:    "private",
		DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteRepo(context.Background(), repo.ID) })

	if _, err := q.GetRepoByOwnerName(ctx, queries.GetRepoByOwnerNameParams{
		Owner: repo.Owner,
		Name:  repo.Name,
	}); err != nil {
		t.Fatalf("get repo by owner/name: %v", err)
	}

	// Grant is an upsert: a second grant updates the level.
	if _, err := q.GrantPermission(ctx, queries.GrantPermissionParams{
		UserID: u.ID, RepoID: repo.ID, Level: "read",
	}); err != nil {
		t.Fatalf("grant read: %v", err)
	}
	perm, err := q.GrantPermission(ctx, queries.GrantPermissionParams{
		UserID: u.ID, RepoID: repo.ID, Level: "write",
	})
	if err != nil {
		t.Fatalf("grant write: %v", err)
	}
	if perm.Level != "write" {
		t.Fatalf("level = %q, want write", perm.Level)
	}

	place, err := q.CreatePlacement(ctx, queries.CreatePlacementParams{
		RepoID:        repo.ID,
		StorageNodeID: "node-1",
	})
	if err != nil {
		t.Fatalf("create placement: %v", err)
	}
	resolved, err := q.ResolvePlacement(ctx, repo.ID)
	if err != nil {
		t.Fatalf("resolve placement: %v", err)
	}
	if resolved.StorageNodeID != place.StorageNodeID {
		t.Fatalf("node = %q, want %q", resolved.StorageNodeID, place.StorageNodeID)
	}
}

func TestInTxRollback(t *testing.T) {
	d, ctx := setup(t)

	username := "user_" + uniq()
	wantErr := context.Canceled
	err := d.InTx(ctx, func(q *queries.Queries) error {
		if _, err := q.CreateUser(ctx, username); err != nil {
			return err
		}
		return wantErr // force rollback
	})
	if err != wantErr {
		t.Fatalf("InTx err = %v, want %v", err, wantErr)
	}

	// The rolled-back user must not be visible.
	if _, err := d.Queries().GetUserByUsername(ctx, username); err == nil {
		t.Fatal("user visible after rollback; expected not found")
	}
}

var uniqCounter atomic.Uint64

// uniq returns a process-unique suffix so parallel/repeated runs don't collide
// on unique constraints.
func uniq() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) +
		"_" + strconv.FormatUint(uniqCounter.Add(1), 36)
}
