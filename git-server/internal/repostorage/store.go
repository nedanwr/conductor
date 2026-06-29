package repostorage

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	"github.com/nedanwr/conductor/git-server/internal/git"
	"github.com/nedanwr/conductor/git-server/internal/repostorage/hooks"
)

// Store is the in-process RepoStorage implementation: bare repos under a storage
// root, pack execution through the git seam, and ref mutation solely through the
// primitive. It satisfies core/repostorage.RepoStorage; the wiring root binds
// either this impl or a Connect client adapter behind that interface, and no
// consumer can tell which.
type Store struct {
	layout    *Layout
	runner    *git.Runner
	primitive *Primitive
}

// NewStore assembles a Store over a storage root and a git runner.
func NewStore(root string, runner *git.Runner) *Store {
	return &Store{
		layout:    NewLayout(root),
		runner:    runner,
		primitive: NewPrimitive(runner),
	}
}

// CreateRepo initializes a bare repo for repoID with the given default branch and
// installs the server-side hooks. It is idempotent on the directory: re-running
// against an existing repo re-initializes safely and refreshes the hooks.
func (s *Store) CreateRepo(ctx context.Context, repoID, defaultBranch string) error {
	path, err := s.layout.Path(repoID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return giterr.Wrap(giterr.KindGitExec, err, "create repo dir %s", path)
	}

	branch := strings.TrimSpace(defaultBranch)
	if branch == "" {
		branch = "main"
	}

	var stderr bytes.Buffer
	if err := s.runner.Run(ctx, git.Spec{
		Args:   []string{"init", "--bare", "--initial-branch", branch, path},
		Env:    gitBaseEnv(),
		Stderr: &stderr,
	}); err != nil {
		return giterr.Wrap(giterr.KindGitExec, err, "init bare repo: %s", strings.TrimSpace(stderr.String()))
	}

	if err := hooks.Install(path); err != nil {
		return err
	}
	return nil
}

// Fetch runs upload-pack for req against its on-disk repo, streaming client input
// from r and pack output to w.
func (s *Store) Fetch(ctx context.Context, req gitreq.GitRequest, r io.Reader, w io.Writer) error {
	path, err := s.resolve(req.RepoID)
	if err != nil {
		return err
	}
	return runUploadPack(ctx, s.runner, path, req.Protocol, r, w)
}

// Receive runs receive-pack for req through the ref-update primitive, so refs
// move only under the per-repo lock with hooks enforcing the rejection rules.
func (s *Store) Receive(ctx context.Context, req gitreq.GitRequest, r io.Reader, w io.Writer) error {
	path, err := s.resolve(req.RepoID)
	if err != nil {
		return err
	}
	return s.primitive.RunReceive(ctx, req.RepoID, path, req.Protocol, gitEnvForRequest(req.Protocol), r, w)
}

// resolve maps a repo UUID to its on-disk path and confirms the repo exists; a
// missing directory is a typed RepoNotFound.
func (s *Store) resolve(repoID string) (string, error) {
	path, err := s.layout.Path(repoID)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", giterr.RepoNotFound("no repo for id %s", repoID)
		}
		return "", giterr.Wrap(giterr.KindGitExec, err, "stat repo %s", path)
	}
	return path, nil
}
