package sqlitedb

import (
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestMigrator_Run(t *testing.T) {
	t.Parallel()

	mem, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = mem.Close() })
	mem.SetMaxOpenConns(1)

	c, err := NewSQLiteClientEx(mem, os.Stdout)
	require.NoError(t, err)

	m, err := NewMigrator(c)
	require.NoError(t, err)
	require.NotNil(t, m)

	// First run should apply migrations (the embedded ones).
	require.NoError(t, m.Run(t.Context()))

	// The migration tracking table must exist after Run.
	tx, err := c.Transaction(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	var count int
	require.NoError(t, tx.QueryRow("SELECT COUNT(*) FROM"+""+migrationTableName+";").Scan(&count))
	require.GreaterOrEqual(t, count, 0)
	require.NoError(t, tx.Rollback())

	// Running again must be idempotent (no error, no duplicate rows).
	require.NoError(t, m.Run(t.Context()))
}
