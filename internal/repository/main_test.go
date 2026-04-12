package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"

	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	_ "modernc.org/sqlite"
)

// newStubDB opens an in-memory SQLite DB and creates the rates table.
func stubSQLiteDB(t interface {
	Helper()
	Cleanup(f func())
}) *sqlitedb.SQLiteClient {
	t.Helper()

	m.Lock()
	defer m.Unlock()

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

	return sqliteDB
}

// mockFailDB implements the db interface but always returns an error from Transaction.
// Use it to test error-handling branches that fire when the DB is unavailable.
type mockFailDB struct{ err error }

func (m *mockFailDB) Transaction(_ context.Context) (*sql.Tx, error) {
	return nil, errors.New(m.err.Error())
}

var m sync.Mutex
