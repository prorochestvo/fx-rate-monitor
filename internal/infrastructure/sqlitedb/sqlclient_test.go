package sqlitedb

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// newTestClient opens an in-memory SQLite DB, applies the migration table, and
// returns a ready-to-use *SQLiteClient. The DB is closed automatically when the
// test finishes.
func newTestClient(t *testing.T) *SQLiteClient {
	t.Helper()

	mem, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = mem.Close() })
	mem.SetMaxOpenConns(1)

	c, err := NewSQLiteClientEx(mem, os.Stdout)
	require.NoError(t, err)

	// Bootstrap the migration table so Ping works.
	m, err := NewMigrator(c)
	require.NoError(t, err)
	require.NoError(t, m.Run(t.Context()))

	return c
}

func TestSQLiteClient_Ping(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)
	require.NoError(t, c.Ping(t.Context()))
}

func TestSQLiteClient_Transaction(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)

	tx, err := c.Transaction(t.Context())
	require.NoError(t, err)
	require.NotNil(t, tx)
	require.NoError(t, tx.Rollback())
}

func TestSQLiteClient_Commit(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)

	// Create a test table first (outside the action under test).
	tx, err := c.Transaction(t.Context())
	require.NoError(t, err)
	_, err = tx.Exec("CREATE TABLE IF NOT EXISTS test_commit (id INTEGER PRIMARY KEY, val TEXT);")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	// Use Commit to insert a row.
	action := &execAction{sql: "INSERT INTO test_commit (val) VALUES ('hello');"}
	require.NoError(t, c.Commit(t.Context(), action))

	// Verify the row was persisted.
	tx2, err := c.Transaction(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx2.Rollback() }()

	var count int
	require.NoError(t, tx2.QueryRow("SELECT COUNT(*) FROM test_commit WHERE val = 'hello';").Scan(&count))
	require.Equal(t, 1, count)
}

func TestSQLiteClient_Rollback(t *testing.T) {
	t.Parallel()

	c := newTestClient(t)

	// Create a test table first.
	tx, err := c.Transaction(t.Context())
	require.NoError(t, err)
	_, err = tx.Exec("CREATE TABLE IF NOT EXISTS test_rollback (id INTEGER PRIMARY KEY, val TEXT);")
	require.NoError(t, err)
	require.NoError(t, tx.Commit())

	// Use Rollback — the insert must NOT be persisted.
	action := &execAction{sql: "INSERT INTO test_rollback (val) VALUES ('world');"}
	require.NoError(t, c.Rollback(t.Context(), action))

	// Verify the row was NOT persisted.
	tx2, err := c.Transaction(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx2.Rollback() }()

	var count int
	require.NoError(t, tx2.QueryRow("SELECT COUNT(*) FROM test_rollback WHERE val = 'world';").Scan(&count))
	require.Equal(t, 0, count)
}

func TestSQLiteClient_Close(t *testing.T) {
	t.Parallel()

	mem, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	mem.SetMaxOpenConns(1)

	c, err := NewSQLiteClientEx(mem, os.Stdout)
	require.NoError(t, err)

	require.NoError(t, c.Close())

	// After Close, the connection should be unusable.
	require.Error(t, mem.Ping())
}

// execAction is a minimal sqlAction for tests.
type execAction struct{ sql string }

func (a *execAction) Run(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, a.sql)
	return err
}
