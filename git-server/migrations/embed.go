// Package migrations embeds the SQL migration files so the binary carries its
// own schema and can apply it at runtime without shipping loose .sql files.
package migrations

import "embed"

// FS holds the embedded migration files.
//
//go:embed *.sql
var FS embed.FS
