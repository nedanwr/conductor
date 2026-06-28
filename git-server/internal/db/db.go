// Package db is Postgres access: the pgx pool and transaction helper, plus the
// goose migration runner (migrate.go). Typed queries are sqlc-generated under
// queries/.
package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/internal/db/queries"
)

// DB owns the pgx connection pool and is the entry point for typed queries. It
// is safe for concurrent use; share a single instance across the process.
type DB struct {
	pool *pgxpool.Pool
}

// Open creates a connection pool from a Postgres DSN and verifies connectivity.
func Open(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, giterr.Wrap(giterr.KindDB, err, "open pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, giterr.Wrap(giterr.KindDB, err, "ping")
	}
	return &DB{pool: pool}, nil
}

// Close releases the pool. Safe to call once during graceful shutdown.
func (d *DB) Close() {
	d.pool.Close()
}

// Pool exposes the underlying pool for callers needing direct access (e.g. the
// migration runner).
func (d *DB) Pool() *pgxpool.Pool { return d.pool }

// Queries returns a query set bound to the pool, for non-transactional use.
func (d *DB) Queries() *queries.Queries {
	return queries.New(d.pool)
}

// InTx runs fn inside a single transaction, committing on success and rolling
// back on error or panic. The query set passed to fn is bound to the tx.
func (d *DB) InTx(ctx context.Context, fn func(*queries.Queries) error) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return giterr.Wrap(giterr.KindDB, err, "begin tx")
	}
	defer func() {
		// Rollback is a no-op once the tx has committed.
		_ = tx.Rollback(ctx)
	}()

	if err := fn(queries.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return giterr.Wrap(giterr.KindDB, err, "commit tx")
	}
	return nil
}
