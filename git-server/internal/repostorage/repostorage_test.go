package repostorage

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	"github.com/nedanwr/conductor/git-server/internal/git"
)

// newRunner asserts the git binary is present — Repo Storage cannot function
// without it, so its absence is a hard test failure, not a skip.
func newRunner(t *testing.T) *git.Runner {
	t.Helper()
	r, err := git.NewRunner()
	if err != nil {
		t.Fatalf("git binary must be present: %v", err)
	}
	return r
}

// runGit runs a plain git command for test setup (independent of the impl under
// test) and fails on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestLayoutPath(t *testing.T) {
	l := NewLayout("/srv/repos")
	got, err := l.Path("abcdef12-3456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join("/srv/repos", "ab", "cd", "abcdef12-3456.git")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}

	for _, bad := range []string{"", "ab", "../etc", "a/b/c"} {
		if _, err := l.Path(bad); giterr.KindOf(err) != giterr.KindRepoNotFound {
			t.Fatalf("Path(%q) kind = %v, want RepoNotFound", bad, giterr.KindOf(err))
		}
	}
}

func TestCreateRepoInstallsHooks(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root, newRunner(t))
	repoID := "11111111-2222-3333-4444-555555555555"

	if err := store.CreateRepo(context.Background(), repoID, "main"); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	path, _ := store.layout.Path(repoID)
	if out := runGit(t, path, "rev-parse", "--is-bare-repository"); out != "true" {
		t.Fatalf("not a bare repo: %q", out)
	}
	if head := runGit(t, path, "symbolic-ref", "HEAD"); head != "refs/heads/main" {
		t.Fatalf("default branch = %q, want refs/heads/main", head)
	}
	hook := filepath.Join(path, "hooks", "pre-receive")
	if _, err := os.Stat(hook); err != nil {
		t.Fatalf("pre-receive hook missing: %v", err)
	}
}

// seedRepo creates a bare repo via the Store and pushes one commit into it,
// returning the bare-repo path and the commit OID on refs/heads/main.
func seedRepo(t *testing.T, store *Store, repoID string) (string, string) {
	t.Helper()
	if err := store.CreateRepo(context.Background(), repoID, "main"); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	barePath, _ := store.layout.Path(repoID)

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "initial")
	runGit(t, work, "remote", "add", "origin", "file://"+barePath)
	runGit(t, work, "push", "origin", "main")

	oid := runGit(t, barePath, "rev-parse", "refs/heads/main")
	return barePath, oid
}

func TestUpdateRefAtomicAndRejection(t *testing.T) {
	store := NewStore(t.TempDir(), newRunner(t))
	repoID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	barePath, oid := seedRepo(t, store, repoID)
	ctx := context.Background()

	// Create a new ref atomically.
	if err := store.primitive.UpdateRef(ctx, repoID, barePath, RefUpdate{Ref: "refs/heads/feature", NewOID: oid}); err != nil {
		t.Fatalf("UpdateRef create: %v", err)
	}
	if got := runGit(t, barePath, "rev-parse", "refs/heads/feature"); got != oid {
		t.Fatalf("feature = %q, want %q", got, oid)
	}

	// A stale compare-and-swap is rejected (atomicity): the wrong old value must
	// not clobber the ref.
	stale := strings.Repeat("0", 39) + "1"
	err := store.primitive.UpdateRef(ctx, repoID, barePath, RefUpdate{Ref: "refs/heads/feature", OldOID: stale, NewOID: oid})
	if giterr.KindOf(err) != giterr.KindRefRejected {
		t.Fatalf("stale CAS kind = %v, want RefRejected", giterr.KindOf(err))
	}

	// A ref outside the allowed namespaces is rejected before touching disk.
	err = store.primitive.UpdateRef(ctx, repoID, barePath, RefUpdate{Ref: "refs/secret/x", NewOID: oid})
	if giterr.KindOf(err) != giterr.KindRefRejected {
		t.Fatalf("bad-namespace kind = %v, want RefRejected", giterr.KindOf(err))
	}
}

// TestPreReceiveHookRejects proves the installed server-side hook enforces the
// rejection rule on a real push: a ref under refs/heads/ is accepted, while a ref
// outside the allowed namespaces is rejected by receive-pack with the hook's
// message.
func TestPreReceiveHookRejects(t *testing.T) {
	store := NewStore(t.TempDir(), newRunner(t))
	repoID := "cccccccc-dddd-eeee-ffff-000000000000"
	if err := store.CreateRepo(context.Background(), repoID, "main"); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	barePath, _ := store.layout.Path(repoID)

	work := t.TempDir()
	runGit(t, work, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "initial")
	runGit(t, work, "remote", "add", "origin", "file://"+barePath)

	// A push into refs/heads/ is accepted.
	runGit(t, work, "push", "origin", "HEAD:refs/heads/ok")

	// A push outside refs/heads/ and refs/tags/ is rejected by the hook.
	cmd := exec.Command("git", "push", "origin", "HEAD:refs/forbidden/x")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("push to forbidden namespace should have failed:\n%s", out)
	}
	if !strings.Contains(string(out), "rejected") {
		t.Fatalf("expected rejection message, got:\n%s", out)
	}
}

// TestReceiveSerializes drives RunReceive against a fake git binary that records
// overlapping execution; serialization means the markers never interleave.
func TestReceiveSerializes(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "marker")
	fakeGit := filepath.Join(t.TempDir(), "git")
	script := "#!/bin/sh\nprintf 'BEGIN\\n' >> \"$MARKER\"\nsleep 0.2\nprintf 'END\\n' >> \"$MARKER\"\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	prim := NewPrimitive(git.NewRunnerWithBin(fakeGit))
	env := append(os.Environ(), "MARKER="+marker)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = prim.RunReceive(ctx, "repo-1", "/unused", gitreq.ProtocolParams{}, env, strings.NewReader(""), &bytes.Buffer{})
		}()
	}
	wg.Wait()

	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(body)); strings.Join(got, " ") != "BEGIN END BEGIN END" {
		t.Fatalf("receive did not serialize: markers = %v", got)
	}
}
