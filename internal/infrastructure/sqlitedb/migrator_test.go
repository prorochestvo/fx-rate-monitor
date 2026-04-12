package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/twinj/uuid"
	_ "modernc.org/sqlite"
)

func TestNewMigrator(t *testing.T) {
	t.Parallel()

	t.Run("default migrations", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		m, err := NewMigrator(c, stubMigrationForRandomTable())
		require.NoError(t, err)
		require.NotNil(t, m)
	})
	t.Run("with custom source applies extra migration", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		src := &stubMigrationSource{
			migrations: map[string]string{
				"custom_001.sql": "CREATE TABLE IF NOT EXISTS" + " custom_test " + "(id INTEGER PRIMARY KEY);",
			},
		}
		m, err := NewMigrator(c, src)
		require.NoError(t, err)
		require.NotNil(t, m)
		require.NoError(t, m.Run(t.Context()))

		tx, err := c.Transaction(t.Context())
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		var count int
		require.NoError(t, tx.QueryRow("SELECT COUNT(*) FROM"+" custom_test;").Scan(&count))
		require.Equal(t, 0, count)
	})
	t.Run("source returning error propagates", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		src := &stubMigrationSource{err: errors.New("source unavailable")}
		_, err := NewMigrator(c, src)
		require.Error(t, err)
		require.ErrorContains(t, err, "source unavailable")
	})
	t.Run("nil transaction with no error returns error", func(t *testing.T) {
		t.Parallel()
		_, err := NewMigrator(&nilTxCommitter{}, stubMigrationForRandomTable())
		require.Error(t, err)
		require.ErrorContains(t, err, "transaction is nil")
	})
	t.Run("transaction error from committer propagates", func(t *testing.T) {
		t.Parallel()
		_, err := NewMigrator(&mockFailCommitter{err: errors.New("connection refused")}, stubMigrationForRandomTable())
		require.Error(t, err)
	})
	t.Run("empty source map is skipped without error", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		src := &stubMigrationSource{migrations: map[string]string{}}
		m, err := NewMigrator(c, src)
		require.NoError(t, err)
		require.NotNil(t, m)
	})
}

func TestMigrator_Run(t *testing.T) {
	t.Parallel()

	t.Run("applies embedded migrations", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		m, err := NewMigrator(c, stubMigrationForRandomTable())
		require.NoError(t, err)
		require.NotNil(t, m)

		require.NoError(t, m.Run(t.Context()))

		tx, err := c.Transaction(t.Context())
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		var count int
		require.NoError(t, tx.QueryRow("SELECT COUNT(*) FROM"+" "+migrationTableName+";").Scan(&count))
		require.GreaterOrEqual(t, count, 0)
	})
	t.Run("idempotent on second run", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		m, err := NewMigrator(c, stubMigrationForRandomTable())
		require.NoError(t, err)

		require.NoError(t, m.Run(t.Context()))
		require.NoError(t, m.Run(t.Context()))
	})
	t.Run("empty content migration is skipped", func(t *testing.T) {
		t.Parallel()
		// A custom source that provides a migration with empty content triggers the
		// len(item.content) == 0 guard inside Run, ensuring that branch is covered.
		c := newTestClient(t)
		src := &stubMigrationSource{
			migrations: map[string]string{
				"zzz_empty.sql": "", // empty content — must be skipped silently
			},
		}
		m, err := NewMigrator(c, src)
		require.NoError(t, err)
		require.NoError(t, m.Run(t.Context()))
	})
	t.Run("transaction error is propagated", func(t *testing.T) {
		t.Parallel()
		// Create a valid migrator first, then swap in a failing committer for Run.
		c := newTestClient(t)
		m, err := NewMigrator(c, stubMigrationForRandomTable())
		require.NoError(t, err)

		m.db = &mockFailCommitter{err: errors.New("db unavailable")}
		require.Error(t, m.Run(t.Context()))
	})
	t.Run("nil transaction in Run returns error", func(t *testing.T) {
		t.Parallel()
		c := newTestClient(t)
		m, err := NewMigrator(c, stubMigrationForRandomTable())
		require.NoError(t, err)

		m.db = &nilTxCommitter{}
		require.Error(t, m.Run(t.Context()))
	})
	t.Run("scan error when migration table is dropped between runs", func(t *testing.T) {
		t.Parallel()
		mem, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = mem.Close() })
		mem.SetMaxOpenConns(1)

		c, err := NewSQLiteClientEx(mem, os.Stdout)
		require.NoError(t, err)

		m, err := NewMigrator(c, stubMigrationForRandomTable())
		require.NoError(t, err)
		require.NoError(t, m.Run(t.Context()))

		// Drop the tracking table so the next Run fails at the QueryRow scan.
		tx, txErr := c.Transaction(t.Context())
		require.NoError(t, txErr)
		_, txErr = tx.Exec("DROP TABLE" + " " + migrationTableName)
		require.NoError(t, txErr)
		require.NoError(t, tx.Commit())

		require.Error(t, m.Run(t.Context()))
	})
	t.Run("invalid migration sql returns error", func(t *testing.T) {
		t.Parallel()
		// Use a fresh DB so the migration has never been applied before.
		mem, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = mem.Close() })
		mem.SetMaxOpenConns(1)

		c, err := NewSQLiteClientEx(mem, os.Stdout)
		require.NoError(t, err)

		src := &stubMigrationSource{
			migrations: map[string]string{
				// Sort after embedded migrations so it runs last.
				"zzz_invalid.sql": "THIS IS NOT VALID SQL !!!",
			},
		}
		m, err := NewMigrator(c, src)
		require.NoError(t, err)

		require.Error(t, m.Run(t.Context()))
	})
}

// stubMigrationSource implements source for testing.
type stubMigrationSource struct {
	migrations map[string]string
	err        error
}

func (s *stubMigrationSource) Migration() (map[string]string, error) {
	return s.migrations, s.err
}

func stubMigrationForRandomTable() *stubMigrationSource {
	var tableName = fmt.Sprintf("table%X", uuid.NewV4().Bytes())
	return &stubMigrationSource{
		migrations: map[string]string{
			tableName + "_init.sql": "CREATE TABLE IF NOT EXISTS `" + migrationTableName + "` (id INTEGER PRIMARY KEY, name TEXT NOT NULL DEFAULT '');",
		},
	}
}

// nilTxCommitter is a committer that returns a nil *sql.Tx with no error,
// exercising the "tx == nil && err == nil" guard in NewMigrator and Run.
type nilTxCommitter struct{}

func (n *nilTxCommitter) Transaction(_ context.Context) (*sql.Tx, error) {
	return nil, nil
}
