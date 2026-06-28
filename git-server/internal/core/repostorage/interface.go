// Package repostorage defines the RepoStorage service interface and its
// serializable boundary types. It holds no implementation and no heavy
// dependencies; impls live in internal/repostorage.
package repostorage

import (
	"context"
	"io"

	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
)

// RepoStorage is the stateful storage tier. Its methods take and return
// serializable types only; no live handle, fd, or pointer crosses the boundary.
// The pack operations are byte streams: callers supply the client→server bytes
// via r and receive the server→client bytes via w. Pktline framing is git's
// concern; this seam shuttles raw bytes.
//
// The wiring root binds this interface to the concrete impl in-process or to a
// Connect client adapter remotely; consumers cannot tell which.
type RepoStorage interface {
	// CreateRepo initializes a bare repo for the given internal UUID with the
	// given default branch.
	CreateRepo(ctx context.Context, repoID, defaultBranch string) error

	// Fetch runs upload-pack for req, reading client input from r and writing
	// pack output to w.
	Fetch(ctx context.Context, req gitreq.GitRequest, r io.Reader, w io.Writer) error

	// Receive runs receive-pack for req, reading client input from r and writing
	// output to w. Refs mutate solely through the ref-update primitive.
	Receive(ctx context.Context, req gitreq.GitRequest, r io.Reader, w io.Writer) error
}
