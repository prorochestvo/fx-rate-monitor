package repository

import (
	"database/sql"
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

var m sync.Mutex
