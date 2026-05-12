// Package sqlitedbtest provides test helpers for packages that need a
// fully-migrated SQLite schema.
package sqlitedbtest

import (
	"context"
	"testing"

	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	"github.com/seilbekskindirov/monitor/migrations"
	"github.com/stretchr/testify/require"
)

// Apply runs every migration in the canonical migrations.MigrationsFS against db.
// It is intended for use in test setup before any repository operations are performed.
// Accepts testing.TB so it works in both Test* and Benchmark* functions.
func Apply(t testing.TB, db sqlitedb.Committer) {
	t.Helper()

	m, err := sqlitedb.NewMigrator(db, migrations.MigrationsFS)
	require.NoError(t, err)
	require.NoError(t, m.Run(context.Background()))
}
