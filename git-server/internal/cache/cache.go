package cache

import (
	"context"
	"io"

	"github.com/nedanwr/conductor/git-server/internal/core/cache"
	"github.com/nedanwr/conductor/git-server/internal/core/gitreq"
	"github.com/nedanwr/conductor/git-server/internal/core/repostorage"
)

// Cache is the read tier. In the slice it is a no-op passthrough: Fetch forwards
// straight through to Repo Storage with no copy held back, so the read path is
// byte-exact whether or not the cache is present. Heat-based promotion turns this
// into a real read-through tier later, behind the same interface. It consumes
// Repo Storage through the core interface, so it can sit in front of an
// in-process Store or a remote client adapter without change.
type Cache struct {
	storage repostorage.RepoStorage
}

// Compile-time check that Cache satisfies the core interface.
var _ cache.Cache = (*Cache)(nil)

// New builds a passthrough Cache over the given Repo Storage.
func New(storage repostorage.RepoStorage) *Cache {
	return &Cache{storage: storage}
}

// Fetch serves upload-pack by passing the request straight through to Repo
// Storage. Writes never reach the cache, so only Fetch is wrapped.
func (c *Cache) Fetch(ctx context.Context, req gitreq.GitRequest, r io.Reader, w io.Writer) error {
	return c.storage.Fetch(ctx, req, r, w)
}
