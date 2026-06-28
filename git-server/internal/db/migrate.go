package db

import (
	"context"
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/nedanwr/conductor/git-server/internal/core/giterr"
	"github.com/nedanwr/conductor/git-server/migrations"
)

const migrationsDir = "."

func init() {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		panic(err)
	}
}

// MigrateUp applies all pending migrations against the given DSN.
func MigrateUp(ctx context.Context, dsn string) error {
	return withGoose(ctx, dsn, func(d *sql.DB) error {
		return goose.UpContext(ctx, d, migrationsDir)
	})
}

// MigrateDown rolls back the most recent migration against the given DSN.
func MigrateDown(ctx context.Context, dsn string) error {
	return withGoose(ctx, dsn, func(d *sql.DB) error {
		return goose.DownContext(ctx, d, migrationsDir)
	})
}

func withGoose(ctx context.Context, dsn string, fn func(*sql.DB) error) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return giterr.Wrap(giterr.KindDB, err, "migrate: open")
	}
	defer sqlDB.Close()

	if err := sqlDB.PingContext(ctx); err != nil {
		return giterr.Wrap(giterr.KindDB, err, "migrate: ping")
	}
	if err := fn(sqlDB); err != nil {
		return giterr.Wrap(giterr.KindDB, err, "migrate")
	}
	return nil
}
