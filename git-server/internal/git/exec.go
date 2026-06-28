// Package git is the runGit(args, cwd) execution seam: it shells to the `git`
// binary and kills the child on context cancel.
package git

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
)

// Runner shells to a resolved `git` binary. It is the sole place the server
// executes git; every higher tier (pack execution, the ref-update primitive)
// goes through it, so process lifetime and cancellation are handled in one spot.
type Runner struct {
	bin string
}

// NewRunner resolves the `git` binary on PATH and returns a Runner bound to it.
// A missing binary is a typed GitExecError so callers surface it on the boundary
// rather than panicking later.
func NewRunner() (*Runner, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, giterr.Wrap(giterr.KindGitExec, err, "git binary not found on PATH")
	}
	return &Runner{bin: bin}, nil
}

// NewRunnerWithBin binds a Runner to an explicit git binary path. Used in tests
// and when the binary location comes from config rather than PATH.
func NewRunnerWithBin(bin string) *Runner {
	return &Runner{bin: bin}
}

// Bin reports the resolved git binary path.
func (r *Runner) Bin() string { return r.bin }

// Spec describes one git invocation. Stdin/Stdout/Stderr may be nil; Env, when
// set, replaces the child environment entirely (callers pass the full set they
// want, never inheriting silently).
type Spec struct {
	Args   []string
	Dir    string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run executes one git invocation and blocks until it exits. The child is bound
// to ctx: on cancel it is killed, and Run waits a short grace period for the
// process to release its pipes before returning so streamed output never leaks a
// goroutine. A non-zero exit is reported as a typed GitExecError.
func (r *Runner) Run(ctx context.Context, spec Spec) error {
	cmd := exec.CommandContext(ctx, r.bin, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdin = spec.Stdin
	cmd.Stdout = spec.Stdout
	cmd.Stderr = spec.Stderr

	// Kill the whole child on cancel and give it a moment to drain before Wait
	// returns, so the attached stdio is not abandoned mid-write.
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	cmd.WaitDelay = 5 * time.Second

	if err := cmd.Run(); err != nil {
		// A context cancellation is the caller's intent, not a git failure;
		// surface it as-is so it maps to a cancellation, not an exec error.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return giterr.Wrap(giterr.KindGitExec, err, "git %v exited %d", spec.Args, exitErr.ExitCode())
		}
		return giterr.Wrap(giterr.KindGitExec, err, "git %v failed to run", spec.Args)
	}
	return nil
}
