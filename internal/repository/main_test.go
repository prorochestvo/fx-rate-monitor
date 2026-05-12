package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb/sqlitedbtest"
	_ "modernc.org/sqlite"
)

var _ sqlitedb.Committer = (*mockFailDB)(nil)

// stubSQLiteDB opens an in-memory SQLite DB, applies the canonical migrations,
// and returns a ready-to-use SQLiteClient. The DB is closed via t.Cleanup.
func stubSQLiteDB(t testing.TB) *sqlitedb.SQLiteClient {
	t.Helper()

	mu.Lock()
	defer mu.Unlock()

	mem, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		panic(err)
	}
	t.Cleanup(func() { _ = mem.Close() })

	mem.SetMaxOpenConns(1)

	sqliteDB, err := sqlitedb.NewSQLiteClientEx(mem, os.Stdout)
	if err != nil {
		panic(err)
	}
	if sqliteDB == nil {
		panic("failed to create SQLite client")
	}

	sqlitedbtest.Apply(t, sqliteDB)

	return sqliteDB
}

// mockFailDB implements the db interface but always returns an error from Transaction.
// Use it to test error-handling branches that fire when the DB is unavailable.
type mockFailDB struct{ err error }

func (m *mockFailDB) Transaction(_ context.Context) (*sql.Tx, error) {
	return nil, errors.New(m.err.Error())
}

var mu sync.Mutex
