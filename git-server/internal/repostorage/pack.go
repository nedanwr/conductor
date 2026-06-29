package repostorage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	"github.com/nedanwr/conductor/git-server/internal/git"
)

// runUploadPack streams upload-pack (the fetch/clone half) for an already-located
// bare repo, reading the client's request bytes from r and writing the pack
// stream to w. It is the SSH-style stdio invocation: the duplex protocol runs
// over the supplied reader/writer; pktline framing is git's concern.
func runUploadPack(ctx context.Context, runner *git.Runner, repoPath string, proto gitreq.ProtocolParams, r io.Reader, w io.Writer) error {
	var stderr bytes.Buffer
	err := runner.Run(ctx, git.Spec{
		Args:   packArgs("upload-pack", repoPath, proto),
		Env:    gitEnvForRequest(proto),
		Stdin:  r,
		Stdout: w,
		Stderr: &stderr,
	})
	if err != nil {
		return giterr.Wrap(giterr.KindGitExec, err, "upload-pack: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

// packArgs builds the pack-program invocation from the request's protocol shape.
// A persistent bidirectional channel runs the program plain (the stateful stdio
// protocol); a stateless exchange runs it with --stateless-rpc, and the opening
// advertisement round adds --advertise-refs. The shape is a wire-protocol
// property, so the same program serves every transport without branching on one.
func packArgs(program, repoPath string, proto gitreq.ProtocolParams) []string {
	args := []string{program}
	if proto.Stateless {
		args = append(args, "--stateless-rpc")
	}
	if proto.AdvertiseRefs {
		args = append(args, "--advertise-refs")
	}
	return append(args, repoPath)
}

// gitBaseEnv is the explicit base environment for a git child. It inherits the
// ambient environment (git needs PATH, and HOME for global config) rather than
// running blind, but every protocol-specific variable is layered on top by the
// caller so nothing leaks in implicitly.
func gitBaseEnv() []string {
	return os.Environ()
}

// gitEnvForRequest builds the child environment for a pack program from the
// negotiated protocol. Protocol v2 is advertised via GIT_PROTOCOL; v0 (the
// fallback) carries no such variable.
func gitEnvForRequest(proto gitreq.ProtocolParams) []string {
	env := gitBaseEnv()
	if proto.Version == 2 {
		env = append(env, fmt.Sprintf("GIT_PROTOCOL=version=%d", proto.Version))
	}
	return env
}
