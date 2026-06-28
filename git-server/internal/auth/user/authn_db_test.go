package user_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/ssh"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/auth/user"
	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	"github.com/nedanwr/conductor/git-server/internal/db"
	"github.com/nedanwr/conductor/git-server/internal/db/queries"
)

func setup(t *testing.T) (*db.DB, context.Context) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed auth test")
	}
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

func newSSHKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh public key: %v", err)
	}
	return sshPub
}

// TestAuthenticateSSHKey seeds a key and resolves it back to its owner; an
// unknown key is a typed Unauthorized.
func TestAuthenticateSSHKey(t *testing.T) {
	d, ctx := setup(t)
	q := d.Queries()
	authn := user.NewAuthenticator(auth.NewStore(q))

	u, err := q.CreateUser(ctx, "user_"+uniq())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteUser(context.Background(), u.ID) })

	key := newSSHKey(t)
	if _, err := q.CreateSSHKey(ctx, queries.CreateSSHKeyParams{
		UserID:      u.ID,
		PublicKey:   string(ssh.MarshalAuthorizedKey(key)),
		Fingerprint: ssh.FingerprintSHA256(key),
	}); err != nil {
		t.Fatalf("create ssh key: %v", err)
	}

	got, err := authn.FromSSHKey(ctx, key)
	if err != nil {
		t.Fatalf("authenticate ssh key: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("resolved user = %v, want %v", got.ID, u.ID)
	}

	if _, err := authn.FromSSHKey(ctx, newSSHKey(t)); giterr.KindOf(err) != giterr.KindUnauthorized {
		t.Fatalf("unknown key kind = %v, want Unauthorized", giterr.KindOf(err))
	}
}

// TestAuthenticateToken seeds a token (by hash) and resolves the raw token to
// its owner; an expired token and an unknown token are both Unauthorized.
func TestAuthenticateToken(t *testing.T) {
	d, ctx := setup(t)
	q := d.Queries()
	authn := user.NewAuthenticator(auth.NewStore(q))

	u, err := q.CreateUser(ctx, "user_"+uniq())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteUser(context.Background(), u.ID) })

	raw := "tok_" + uniq()
	if _, err := q.CreateAccessToken(ctx, queries.CreateAccessTokenParams{
		UserID:    u.ID,
		TokenHash: auth.HashToken(raw),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}); err != nil {
		t.Fatalf("create token: %v", err)
	}

	got, err := authn.FromToken(ctx, raw)
	if err != nil {
		t.Fatalf("authenticate token: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("resolved user = %v, want %v", got.ID, u.ID)
	}

	expiredRaw := "tok_" + uniq()
	if _, err := q.CreateAccessToken(ctx, queries.CreateAccessTokenParams{
		UserID:    u.ID,
		TokenHash: auth.HashToken(expiredRaw),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	}); err != nil {
		t.Fatalf("create expired token: %v", err)
	}
	if _, err := authn.FromToken(ctx, expiredRaw); giterr.KindOf(err) != giterr.KindUnauthorized {
		t.Fatalf("expired token kind = %v, want Unauthorized", giterr.KindOf(err))
	}

	if _, err := authn.FromToken(ctx, "tok_"+uniq()); giterr.KindOf(err) != giterr.KindUnauthorized {
		t.Fatalf("unknown token kind = %v, want Unauthorized", giterr.KindOf(err))
	}
}

// TestCanEndToEnd exercises the authorizer against real rows: an unauthorized
// push is rejected as a typed Unauthorized, a granted push is allowed, and an
// anonymous fetch of a public repo is allowed.
func TestCanEndToEnd(t *testing.T) {
	d, ctx := setup(t)
	q := d.Queries()
	authz := user.NewAuthorizer(auth.NewStore(q))

	u, err := q.CreateUser(ctx, "user_"+uniq())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteUser(context.Background(), u.ID) })

	priv, err := q.CreateRepo(ctx, queries.CreateRepoParams{
		Owner: "owner_" + uniq(), Name: "repo", Visibility: "private", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create private repo: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteRepo(context.Background(), priv.ID) })

	principal := auth.User{ID: u.ID, Username: u.Username}

	// No permission yet: push is rejected as Unauthorized.
	if _, err := authz.Can(ctx, principal, priv.ID, gitreq.OperationReceive); giterr.KindOf(err) != giterr.KindUnauthorized {
		t.Fatalf("ungranted push kind = %v, want Unauthorized", giterr.KindOf(err))
	}

	// Grant write: push is now allowed at write level.
	if _, err := q.GrantPermission(ctx, queries.GrantPermissionParams{
		UserID: u.ID, RepoID: priv.ID, Level: "write",
	}); err != nil {
		t.Fatalf("grant write: %v", err)
	}
	grant, err := authz.Can(ctx, principal, priv.ID, gitreq.OperationReceive)
	if err != nil {
		t.Fatalf("granted push: %v", err)
	}
	if grant.Level != gitreq.GrantLevelWrite {
		t.Fatalf("grant level = %v, want write", grant.Level)
	}

	// Anonymous fetch of a public repo is allowed at read level.
	pub, err := q.CreateRepo(ctx, queries.CreateRepoParams{
		Owner: "owner_" + uniq(), Name: "repo", Visibility: "public", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create public repo: %v", err)
	}
	t.Cleanup(func() { _ = q.DeleteRepo(context.Background(), pub.ID) })

	grant, err = authz.Can(ctx, auth.Anonymous, pub.ID, gitreq.OperationFetch)
	if err != nil {
		t.Fatalf("anonymous public fetch: %v", err)
	}
	if grant.Level != gitreq.GrantLevelRead {
		t.Fatalf("anonymous fetch grant = %v, want read", grant.Level)
	}
}

var uniqCounter atomic.Uint64

func uniq() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "_" + strconv.FormatUint(uniqCounter.Add(1), 36)
}
