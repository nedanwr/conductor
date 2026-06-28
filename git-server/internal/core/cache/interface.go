// Package cache defines the Cache service interface and its serializable
// boundary types. Interface only, no impl.
package cache

import (
	"context"
	"io"

	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
)

// Cache is the read tier. It wraps reads through to Repo Storage; in the slice
// it is a no-op passthrough and heat-based promotion is deferred. Only the read
// path goes through the cache; writes bypass it, so the interface exposes Fetch
// alone. The byte-stream seam mirrors repostorage.RepoStorage.Fetch.
type Cache interface {
	// Fetch serves upload-pack for req, reading client input from r and writing
	// pack output to w — passing through to Repo Storage in the slice.
	Fetch(ctx context.Context, req gitreq.GitRequest, r io.Reader, w io.Writer) error
}
