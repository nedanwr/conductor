package https_test

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nedanwr/conductor/git-server/internal/auth"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	coreregistry "github.com/nedanwr/conductor/git-server/internal/core/registry"
	"github.com/nedanwr/conductor/git-server/internal/gateway"
	"github.com/nedanwr/conductor/git-server/internal/gateway/https"
	"github.com/nedanwr/conductor/git-server/internal/git"
	"github.com/nedanwr/conductor/git-server/internal/repostorage"
)

// These fakes stand in for the control-plane wiring (resolved later at the
// wiring root). They isolate the smart-HTTP bridge so a real git client exercises
// the real pack programs end to end without a database.

type resolver struct{ id uuid.UUID }

func (r resolver) Resolve(context.Context, string, string) (uuid.UUID, error) { return r.id, nil }

type allowAll struct{}

func (allowAll) Can(context.Context, auth.User, uuid.UUID, gitreq.Operation) (gitreq.Grant, error) {
	return gitreq.Grant{Level: gitreq.GrantLevelWrite}, nil
}

type oneNode struct{}

func (oneNode) ResolvePlacement(context.Context, string) (coreregistry.Node, error) {
	return coreregistry.Node{ID: "node-1"}, nil
}
func (oneNode) CreatePlacement(context.Context, string, string) (coreregistry.Node, error) {
	return coreregistry.Node{ID: "node-1"}, nil
}

type noToken struct{}

func (noToken) FromToken(context.Context, string) (auth.User, error) { return auth.Anonymous, nil }

// TestCloneAndPushOverHTTPS drives a real git client through the smart-HTTP
// terminator against the real RepoStorage store, proving the stateless-rpc and
// advertisement bridge is byte-correct: a clone, a push, and a re-clone that sees
// the pushed commit.
func TestCloneAndPushOverHTTPS(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not on PATH")
	}

	root := t.TempDir()
	store := repostorage.NewStore(root, git.NewRunnerWithBin(gitBin))
	repoID := uuid.New()
	if err := store.CreateRepo(context.Background(), repoID.String(), "main"); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	gw := gateway.New(resolver{id: repoID}, allowAll{}, oneNode{}, gateway.NewSingleRouter("node-1", store))
	srv := httptest.NewServer(https.NewHandler(gw, noToken{}))
	defer srv.Close()

	repoURL := srv.URL + "/alice/proj.git"
	for _, version := range []string{"version=2", "version=0"} {
		t.Run(version, func(t *testing.T) {
			work := t.TempDir()

			// Clone the (empty) repo.
			clone := filepath.Join(work, "clone")
			runGit(t, work, version, "clone", repoURL, clone)

			// Make a commit and push it.
			if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("hello "+version), 0o644); err != nil {
				t.Fatal(err)
			}
			runGit(t, clone, version, "add", "README.md")
			runGit(t, clone, version, "-c", "user.email=a@b.c", "-c", "user.name=A", "commit", "-m", "init")
			runGit(t, clone, version, "push", "origin", "HEAD:refs/heads/"+branchFor(version))

			// Re-clone and confirm the pushed commit is present.
			fresh := filepath.Join(work, "fresh")
			runGit(t, work, version, "clone", "--branch", branchFor(version), repoURL, fresh)
			if got := runGit(t, fresh, version, "log", "-1", "--pretty=%s"); got != "init" {
				t.Fatalf("re-clone missing pushed commit: subject = %q", got)
			}
		})
	}
}

// branchFor keeps the two protocol sub-tests on separate branches so they share
// the one bare repo without colliding.
func branchFor(version string) string {
	if version == "version=2" {
		return "v2"
	}
	return "v0"
}

func runGit(t *testing.T, dir, version string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"GIT_PROTOCOL=" + version,
		"GIT_TERMINAL_PROMPT=0",
		"HOME=" + dir,
		"GIT_CONFIG_NOSYSTEM=1",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
