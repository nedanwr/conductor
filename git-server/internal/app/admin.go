package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/ssh"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/db"
	"github.com/nedanwr/conductor/git-server/internal/db/queries"
)

// adminUsage describes the provisioning verbs the artifact runs out-of-band.
const adminUsage = `usage: git-server admin <command>

commands:
  user create <username>
  key add <username> <public-key-file|->
  token create <username> [--expires <duration>]
  repo create <owner>/<name> [--visibility private|public] [--branch <name>] [--node <id>]
  grant <username> <owner>/<name> <read|write|admin>`

// Admin runs a single provisioning verb and exits. It is not a service: it starts
// no listeners, plays no runtime role, and branches on the verb rather than on
// mode. Its trust boundary is operational — whoever can execute the binary and
// reach Postgres — so it performs no self-authentication. Each verb is a thin
// call into the same auth and registry code the running services use, never a
// parallel implementation of user, key, repo, or grant logic.
func Admin(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New(adminUsage)
	}
	cfg := LoadConfig(ModeAll)
	switch args[0] {
	case "user":
		return adminUser(ctx, cfg, args[1:])
	case "key":
		return adminKey(ctx, cfg, args[1:])
	case "token":
		return adminToken(ctx, cfg, args[1:])
	case "repo":
		return adminRepo(ctx, cfg, args[1:])
	case "grant":
		return adminGrant(ctx, cfg, args[1:])
	default:
		return fmt.Errorf("unknown admin command %q\n\n%s", args[0], adminUsage)
	}
}

// adminUser handles `user create`.
func adminUser(ctx context.Context, cfg Config, args []string) error {
	if len(args) != 2 || args[0] != "create" {
		return errors.New("usage: git-server admin user create <username>")
	}
	username := args[1]

	database, closeDB, err := adminDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB()

	u, err := database.Queries().CreateUser(ctx, username)
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	fmt.Printf("created user %s (%s)\n", u.Username, u.ID)
	return nil
}

// adminKey handles `key add`, registering an SSH public key for a user so they
// can authenticate on the git SSH path.
func adminKey(ctx context.Context, cfg Config, args []string) error {
	if len(args) != 3 || args[0] != "add" {
		return errors.New("usage: git-server admin key add <username> <public-key-file|->")
	}
	username, source := args[1], args[2]

	raw, err := readKeyMaterial(source)
	if err != nil {
		return err
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey(raw)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	database, closeDB, err := adminDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB()

	u, err := userByName(ctx, database, username)
	if err != nil {
		return err
	}
	key, err := database.Queries().CreateSSHKey(ctx, queries.CreateSSHKeyParams{
		UserID:      u.ID,
		PublicKey:   strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))),
		Fingerprint: ssh.FingerprintSHA256(pub),
	})
	if err != nil {
		return fmt.Errorf("add key: %w", err)
	}
	fmt.Printf("added key %s for %s (%s)\n", key.Fingerprint, u.Username, u.ID)
	return nil
}

// adminToken handles `token create`, minting an HTTPS access token for a user.
// The raw token is shown once here and never stored; only its hash is persisted.
func adminToken(ctx context.Context, cfg Config, args []string) error {
	if len(args) < 2 || args[0] != "create" {
		return errors.New("usage: git-server admin token create <username> [--expires <duration>]")
	}
	username := args[1]

	fs := flag.NewFlagSet("token create", flag.ContinueOnError)
	expires := fs.Duration("expires", 0, "token lifetime (0 = no expiry)")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}

	database, closeDB, err := adminDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB()

	u, err := userByName(ctx, database, username)
	if err != nil {
		return err
	}

	raw, err := mintToken()
	if err != nil {
		return err
	}
	var expiresAt pgtype.Timestamptz
	if *expires > 0 {
		expiresAt = pgtype.Timestamptz{Time: time.Now().Add(*expires), Valid: true}
	}
	if _, err := database.Queries().CreateAccessToken(ctx, queries.CreateAccessTokenParams{
		UserID:    u.ID,
		TokenHash: auth.HashToken(raw),
		ExpiresAt: expiresAt,
	}); err != nil {
		return fmt.Errorf("create token: %w", err)
	}
	fmt.Printf("token for %s (shown once): %s\n", u.Username, raw)
	return nil
}

// adminRepo handles `repo create`: it records the repo, places it on a storage
// node, and initializes the bare repository on that node's disk — the three steps
// that make a repo addressable, routable, and present.
func adminRepo(ctx context.Context, cfg Config, args []string) error {
	if len(args) < 2 || args[0] != "create" {
		return errors.New("usage: git-server admin repo create <owner>/<name> [--visibility ...] [--branch ...] [--node ...]")
	}
	address := args[1]

	fs := flag.NewFlagSet("repo create", flag.ContinueOnError)
	visibility := fs.String("visibility", "private", "repo visibility: private|public")
	branch := fs.String("branch", "main", "default branch")
	node := fs.String("node", "", "storage node id (empty = local node)")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	owner, name, ok := strings.Cut(address, "/")
	if !ok || owner == "" || name == "" {
		return fmt.Errorf("invalid repo address %q: want owner/name", address)
	}
	vis, err := auth.ParseVisibility(*visibility)
	if err != nil {
		return err
	}

	database, closeDB, err := adminDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB()

	repo, err := database.Queries().CreateRepo(ctx, queries.CreateRepoParams{
		Owner:         owner,
		Name:          name,
		Visibility:    vis.String(),
		DefaultBranch: *branch,
	})
	if err != nil {
		return fmt.Errorf("create repo: %w", err)
	}

	placed, err := localRegistry(database, cfg).CreatePlacement(ctx, repo.ID.String(), *node)
	if err != nil {
		return fmt.Errorf("place repo: %w", err)
	}

	store, err := newStore(cfg)
	if err != nil {
		return err
	}
	if err := store.CreateRepo(ctx, repo.ID.String(), repo.DefaultBranch); err != nil {
		return fmt.Errorf("initialize repo on disk: %w", err)
	}

	fmt.Printf("created repo %s/%s (%s) on node %s, default branch %s\n",
		repo.Owner, repo.Name, repo.ID, placed.ID, repo.DefaultBranch)
	return nil
}

// adminGrant handles `grant`, recording a user's permission level on a repo.
func adminGrant(ctx context.Context, cfg Config, args []string) error {
	if len(args) != 3 {
		return errors.New("usage: git-server admin grant <username> <owner>/<name> <read|write|admin>")
	}
	username, address, levelStr := args[0], args[1], args[2]
	owner, name, ok := strings.Cut(address, "/")
	if !ok || owner == "" || name == "" {
		return fmt.Errorf("invalid repo address %q: want owner/name", address)
	}
	level, err := auth.ParseLevel(levelStr)
	if err != nil {
		return err
	}

	database, closeDB, err := adminDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeDB()

	u, err := userByName(ctx, database, username)
	if err != nil {
		return err
	}
	repo, err := database.Queries().GetRepoByOwnerName(ctx, queries.GetRepoByOwnerNameParams{Owner: owner, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no repo %s/%s", owner, name)
	}
	if err != nil {
		return fmt.Errorf("lookup repo: %w", err)
	}

	if _, err := database.Queries().GrantPermission(ctx, queries.GrantPermissionParams{
		UserID: u.ID,
		RepoID: repo.ID,
		Level:  level.String(),
	}); err != nil {
		return fmt.Errorf("grant permission: %w", err)
	}
	fmt.Printf("granted %s on %s/%s to %s\n", level, repo.Owner, repo.Name, u.Username)
	return nil
}

// adminDB opens the Postgres pool for an admin verb, applying pending migrations
// first so provisioning works against a fresh database. The returned closer
// releases the pool.
func adminDB(ctx context.Context, cfg Config) (*db.DB, func(), error) {
	if cfg.DatabaseDSN == "" {
		return nil, nil, errors.New("database DSN is required (set DATABASE_URL)")
	}
	if err := db.MigrateUp(ctx, cfg.DatabaseDSN); err != nil {
		return nil, nil, err
	}
	database, err := db.Open(ctx, cfg.DatabaseDSN)
	if err != nil {
		return nil, nil, err
	}
	return database, database.Close, nil
}

// userByName resolves a username to the stored user, reporting a clear error when
// no such user exists.
func userByName(ctx context.Context, database *db.DB, username string) (queries.User, error) {
	u, err := database.Queries().GetUserByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return queries.User{}, fmt.Errorf("no user %q", username)
	}
	if err != nil {
		return queries.User{}, fmt.Errorf("lookup user: %w", err)
	}
	return u, nil
}

// readKeyMaterial reads an SSH public key from a file path, or from stdin when
// the source is "-".
func readKeyMaterial(source string) ([]byte, error) {
	if source == "-" {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read key from stdin: %w", err)
		}
		return raw, nil
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	return raw, nil
}

// mintToken generates a random opaque access token.
func mintToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
