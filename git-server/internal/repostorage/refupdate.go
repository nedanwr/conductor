package repostorage

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/git"
)

// zeroOID is git's all-zero object id, used to express ref creation (as old) and
// deletion (as new).
const zeroOID = "0000000000000000000000000000000000000000"

// RefUpdate names a single ref mutation. An empty OldOID skips the
// compare-and-swap precondition; a zero or empty NewOID deletes the ref.
type RefUpdate struct {
	Ref    string
	OldOID string
	NewOID string
}

// Primitive is the sole ref mutator. Every ref movement — whether driven by a
// push (receive-pack, below) or constructed by a future write path (merge/PR) —
// passes through here, so per-repo serialization, native per-ref lockfiles, and
// the rejection rules live in exactly one place. Concurrent receives against one
// repo are serialized by a per-repo lock keyed on the repo UUID; git's own
// lockfiles then give per-ref atomicity within a serialized push.
type Primitive struct {
	runner *git.Runner

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewPrimitive builds the ref-update primitive over a git runner.
func NewPrimitive(runner *git.Runner) *Primitive {
	return &Primitive{runner: runner, locks: make(map[string]*sync.Mutex)}
}

// lockFor returns the serialization lock for a repo, creating it on first use.
func (p *Primitive) lockFor(repoID string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	l, ok := p.locks[repoID]
	if !ok {
		l = &sync.Mutex{}
		p.locks[repoID] = l
	}
	return l
}

// RunReceive executes receive-pack for one push under the repo's serialization
// lock, streaming client bytes in and server bytes out. The repo's pre-receive
// hook enforces the rejection rules during this call; git's native lockfiles make
// each accepted ref update atomic. This is the receive-pack door onto the
// primitive: refs only move because receive-pack runs here.
func (p *Primitive) RunReceive(ctx context.Context, repoID, repoPath string, env []string, r io.Reader, w io.Writer) error {
	lock := p.lockFor(repoID)
	lock.Lock()
	defer lock.Unlock()

	var stderr bytes.Buffer
	err := p.runner.Run(ctx, git.Spec{
		Args:   []string{"receive-pack", repoPath},
		Env:    env,
		Stdin:  r,
		Stdout: w,
		Stderr: &stderr,
	})
	if err != nil {
		return giterr.Wrap(giterr.KindGitExec, err, "receive-pack: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

// UpdateRef moves a single ref directly, the second door onto the primitive (the
// merge/PR write path uses it later). It applies the same rejection rules as the
// push path and, when OldOID is set, performs a compare-and-swap so a stale
// caller cannot clobber a concurrent update. The move is serialized per repo.
func (p *Primitive) UpdateRef(ctx context.Context, repoID, repoPath string, upd RefUpdate) error {
	if err := validateRefName(upd.Ref); err != nil {
		return err
	}

	lock := p.lockFor(repoID)
	lock.Lock()
	defer lock.Unlock()

	var args []string
	if upd.NewOID == "" || upd.NewOID == zeroOID {
		args = []string{"update-ref", "-d", upd.Ref}
		if upd.OldOID != "" {
			args = append(args, upd.OldOID)
		}
	} else {
		args = []string{"update-ref", upd.Ref, upd.NewOID}
		if upd.OldOID != "" {
			args = append(args, upd.OldOID)
		}
	}

	var stderr bytes.Buffer
	err := p.runner.Run(ctx, git.Spec{
		Args:   args,
		Dir:    repoPath,
		Env:    gitBaseEnv(),
		Stderr: &stderr,
	})
	if err != nil {
		// A failed compare-and-swap or a rejected move is a precondition failure,
		// not an internal git fault.
		return giterr.Wrap(giterr.KindRefRejected, err, "update-ref %s: %s", upd.Ref, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// validateRefName is the baseline rejection rule shared by both doors: a ref must
// live under refs/heads/ or refs/tags/ and be free of the components git forbids.
func validateRefName(ref string) error {
	if !strings.HasPrefix(ref, "refs/heads/") && !strings.HasPrefix(ref, "refs/tags/") {
		return giterr.RefRejected("ref %q is outside refs/heads/ and refs/tags/", ref)
	}
	if strings.Contains(ref, "..") || strings.ContainsAny(ref, " \t\n~^:?*[\\") || strings.HasSuffix(ref, "/") {
		return giterr.RefRejected("ref %q is malformed", ref)
	}
	return nil
}
