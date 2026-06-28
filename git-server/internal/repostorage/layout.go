// Package repostorage is the stateful Repo Storage service: bare sharded repos
// on disk, upload-pack/receive-pack execution, and the ref-update primitive —
// the sole ref mutator. It implements core/repostorage.
package repostorage

import (
	"path/filepath"
	"strings"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
)

// Layout maps a repo UUID to its on-disk bare-repo path under a storage root.
// Repos are sharded by the leading bytes of their UUID so a single directory
// never holds the whole fleet; the UUID — not owner/repo — is the on-disk
// identity, matching the boundary where the Gateway has already resolved names.
type Layout struct {
	root string
}

// NewLayout binds a Layout to the storage root directory.
func NewLayout(root string) *Layout {
	return &Layout{root: root}
}

// Root reports the storage root.
func (l *Layout) Root() string { return l.root }

// Path returns the absolute bare-repo path for repoID. The UUID is sharded into
// two two-character segments (e.g. ab/cd/abcd…-….git) to bound directory fan-out.
// A malformed UUID is a typed RepoNotFound, since nothing on disk can resolve it.
func (l *Layout) Path(repoID string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(repoID))
	// Guard against path traversal and obviously invalid identities before any
	// of it reaches the filesystem.
	if len(id) < 4 || strings.ContainsAny(id, "/\\.") {
		return "", giterr.RepoNotFound("invalid repo id %q", repoID)
	}
	return filepath.Join(l.root, id[0:2], id[2:4], id+".git"), nil
}
