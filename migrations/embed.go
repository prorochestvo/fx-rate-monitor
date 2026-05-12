// Package migrations exposes the canonical SQL migration files via embed.FS so
// every consumer (cmd/migrator, repository tests) reads from a single source.
package migrations

import "embed"

//go:embed *.sql
var MigrationsFS embed.FS
