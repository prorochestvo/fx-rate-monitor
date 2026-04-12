package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
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

func TestNewSQLiteClientEx(t *testing.T) {
	t.Parallel()

	t.Run("returns error when db is already closed", func(t *testing.T) {
		t.Parallel()
		mem, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		require.NoError(t, mem.Close())
		_, err = NewSQLiteClientEx(mem, os.Stdout)
		require.Error(t, err)
	})
}

func TestNewSQLiteClient(t *testing.T) {
	t.Parallel()

	t.Run("opens a file-based sqlite database", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")
		dsn := &stubDataSource{path: dbPath}

		c, err := NewSQLiteClient(dsn, os.Stdout)
		require.NoError(t, err)
		require.NotNil(t, c)
		t.Cleanup(func() { _ = c.Close() })

		// Bootstrap migration table so Ping (which queries it) works.
		m, err := NewMigrator(c)
		require.NoError(t, err)
		require.NoError(t, m.Run(t.Context()))

		require.NoError(t, c.Ping(t.Context()))
	})
	t.Run("returns error when database path is inaccessible", func(t *testing.T) {
		t.Parallel()
		// A path under a non-existent directory forces the SQLite driver to fail
		// when executing the first statement (WAL/foreign-key pragmas inside
		// NewSQLiteClientEx), exercising the constructor error path.
		dsn := &stubDataSource{path: "/nonexistent/path/that/cannot/be/created/test.db"}
		_, err := NewSQLiteClient(dsn, os.Stdout)
		require.Error(t, err)
	})
}

func TestSQLiteClient_Ping(t *testing.T) {
	t.Parallel()

	t.Run("succeeds on valid client", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		require.NoError(t, c.Ping(t.Context()))
	})
	t.Run("returns error when migration table is absent", func(t *testing.T) {
		t.Parallel()
		// A freshly-created client without running the migrator has no
		// __schema_migrations table. Ping queries it, so it must fail.
		mem, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = mem.Close() })
		mem.SetMaxOpenConns(1)
		c, err := NewSQLiteClientEx(mem, os.Stdout)
		require.NoError(t, err)
		require.Error(t, c.Ping(t.Context()))
	})
	t.Run("returns error on closed db", func(t *testing.T) {
		t.Parallel()
		mem, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		mem.SetMaxOpenConns(1)
		c, err := NewSQLiteClientEx(mem, os.Stdout)
		require.NoError(t, err)
		require.NoError(t, mem.Close())
		require.Error(t, c.Ping(t.Context()))
	})
}

func TestSQLiteClient_Transaction(t *testing.T) {
	t.Parallel()

	t.Run("returns valid transaction", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		tx, err := c.Transaction(t.Context())
		require.NoError(t, err)
		require.NotNil(t, tx)
		require.NoError(t, tx.Rollback())
	})
}

func TestSQLiteClient_Commit(t *testing.T) {
	t.Parallel()

	setupTable := func(t *testing.T, c *SQLiteClient, tableName string) {
		t.Helper()
		tx, err := c.Transaction(t.Context())
		require.NoError(t, err)
		_, err = tx.Exec("CREATE TABLE IF NOT EXISTS " + tableName + " (id INTEGER PRIMARY KEY, val TEXT);")
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	}

	t.Run("single action is committed", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		setupTable(t, c, "test_commit_single")

		action := &execAction{sql: "INSERT INTO test_commit_single (val) VALUES ('hello');"}
		require.NoError(t, c.Commit(t.Context(), action))

		tx2, err := c.Transaction(t.Context())
		require.NoError(t, err)
		defer func() { _ = tx2.Rollback() }()

		var count int
		require.NoError(t, tx2.QueryRow("SELECT COUNT(*) FROM test_commit_single WHERE val = 'hello';").Scan(&count))
		require.Equal(t, 1, count)
	})
	t.Run("extra action is also committed", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		setupTable(t, c, "test_commit_extra")

		a1 := &execAction{sql: "INSERT INTO test_commit_extra (val) VALUES ('first');"}
		a2 := &execAction{sql: "INSERT INTO test_commit_extra (val) VALUES ('second');"}
		require.NoError(t, c.Commit(t.Context(), a1, a2))

		tx2, err := c.Transaction(t.Context())
		require.NoError(t, err)
		defer func() { _ = tx2.Rollback() }()

		var count int
		require.NoError(t, tx2.QueryRow("SELECT COUNT(*) FROM test_commit_extra;").Scan(&count))
		require.Equal(t, 2, count)
	})
	t.Run("primary action failure returns error", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		require.Error(t, c.Commit(t.Context(), &errAction{err: errors.New("primary failed")}))
	})
	t.Run("extra action failure returns error", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		setupTable(t, c, "test_commit_fail")

		a1 := &execAction{sql: "INSERT INTO test_commit_fail (val) VALUES ('first');"}
		a2 := &errAction{err: errors.New("action failed")}
		require.Error(t, c.Commit(t.Context(), a1, a2))

		tx2, err := c.Transaction(t.Context())
		require.NoError(t, err)
		defer func() { _ = tx2.Rollback() }()

		var count int
		require.NoError(t, tx2.QueryRow("SELECT COUNT(*) FROM test_commit_fail;").Scan(&count))
		require.Equal(t, 0, count)
	})
	t.Run("returns error when db is closed", func(t *testing.T) {
		t.Parallel()
		mem, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		mem.SetMaxOpenConns(1)
		c, err := NewSQLiteClientEx(mem, os.Stdout)
		require.NoError(t, err)
		require.NoError(t, mem.Close())
		require.Error(t, c.Commit(t.Context(), &execAction{sql: "SELECT 1;"}))
	})
}

func TestSQLiteClient_Rollback(t *testing.T) {
	t.Parallel()

	setupTable := func(t *testing.T, c *SQLiteClient, tableName string) {
		t.Helper()
		tx, err := c.Transaction(t.Context())
		require.NoError(t, err)
		_, err = tx.Exec("CREATE TABLE IF NOT EXISTS " + tableName + " (id INTEGER PRIMARY KEY, val TEXT);")
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
	}

	t.Run("single action is not persisted", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		setupTable(t, c, "test_rollback_single")

		action := &execAction{sql: "INSERT INTO test_rollback_single (val) VALUES ('world');"}
		require.NoError(t, c.Rollback(t.Context(), action))

		tx2, err := c.Transaction(t.Context())
		require.NoError(t, err)
		defer func() { _ = tx2.Rollback() }()

		var count int
		require.NoError(t, tx2.QueryRow("SELECT COUNT(*) FROM test_rollback_single WHERE val = 'world';").Scan(&count))
		require.Equal(t, 0, count)
	})
	t.Run("extra action is also not persisted", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		setupTable(t, c, "test_rollback_extra")

		a1 := &execAction{sql: "INSERT INTO test_rollback_extra (val) VALUES ('first');"}
		a2 := &execAction{sql: "INSERT INTO test_rollback_extra (val) VALUES ('second');"}
		require.NoError(t, c.Rollback(t.Context(), a1, a2))

		tx2, err := c.Transaction(t.Context())
		require.NoError(t, err)
		defer func() { _ = tx2.Rollback() }()

		var count int
		require.NoError(t, tx2.QueryRow("SELECT COUNT(*) FROM test_rollback_extra;").Scan(&count))
		require.Equal(t, 0, count)
	})
	t.Run("primary action failure returns error", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		require.Error(t, c.Rollback(t.Context(), &errAction{err: errors.New("primary failed")}))
	})
	t.Run("returns error when db is closed", func(t *testing.T) {
		t.Parallel()
		mem, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		mem.SetMaxOpenConns(1)
		c, err := NewSQLiteClientEx(mem, os.Stdout)
		require.NoError(t, err)
		require.NoError(t, mem.Close())
		require.Error(t, c.Rollback(t.Context(), &execAction{sql: "SELECT 1;"}))
	})
}

func TestSQLiteClient_Vacuum(t *testing.T) {
	t.Parallel()

	t.Run("succeeds on valid client", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		require.NoError(t, c.Vacuum(t.Context()))
	})
}

func TestSQLiteClient_Close(t *testing.T) {
	t.Parallel()

	t.Run("closes successfully and makes db unusable", func(t *testing.T) {
		t.Parallel()
		mem, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		mem.SetMaxOpenConns(1)

		c, err := NewSQLiteClientEx(mem, os.Stdout)
		require.NoError(t, err)

		require.NoError(t, c.Close())
		require.Error(t, mem.Ping())
	})
}

// execAction is a minimal sqlAction for tests.
type execAction struct{ sql string }

func (a *execAction) Run(tx *sql.Tx, ctx context.Context) error {
	_, err := tx.ExecContext(ctx, a.sql)
	return err
}

// errAction is a sqlAction that always returns the configured error.
type errAction struct{ err error }

func (a *errAction) Run(_ *sql.Tx, _ context.Context) error {
	return a.err
}

// mockFailCommitter simulates a committer whose Transaction call always fails.
type mockFailCommitter struct{ err error }

func (m *mockFailCommitter) Transaction(_ context.Context) (*sql.Tx, error) {
	return nil, m.err
}

// stubDataSource implements dsninjector.DataSource for testing by returning a
// fixed file path from Database().
type stubDataSource struct{ path string }

func (s *stubDataSource) Database() string                    { return s.path }
func (s *stubDataSource) Addr(_ ...int) string                { return "" }
func (s *stubDataSource) AuthBasicBase64() string             { return "" }
func (s *stubDataSource) Driver() string                      { return "sqlite" }
func (s *stubDataSource) Host() string                        { return "" }
func (s *stubDataSource) Login() string                       { return "" }
func (s *stubDataSource) Option(_ string, _ ...string) string { return "" }
func (s *stubDataSource) OptionsNames() []string              { return nil }
func (s *stubDataSource) Password() string                    { return "" }
func (s *stubDataSource) Port() int                           { return 0 }
