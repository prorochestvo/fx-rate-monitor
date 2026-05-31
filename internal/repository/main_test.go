package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb/sqlitedbtest"
	_ "modernc.org/sqlite"
)

var _ sqlitedb.Committer = (*mockFailDB)(nil)

// stubSQLiteDB opens an in-memory SQLite DB, applies the canonical migrations,
// and returns a ready-to-use SQLiteClient. The DB is closed via t.Cleanup.
//
// Optional sourceNames are pre-seeded into rate_sources via seedRateSources so
// dependent rows (rate_values, rate_user_subscriptions, rate_user_events) can
// satisfy the FK on rate_user_subscriptions.source_name. Tests using custom
// source names outside the canonical seed should pass them here.
//
// The shared mutex guards only the sql.Open + PRAGMA + migrate phase; seeding
// proceeds without it so parallel tests don't serialise behind each other's
// N source inserts.
func stubSQLiteDB(t testing.TB, sourceNames ...string) *sqlitedb.SQLiteClient {
	t.Helper()

	sqliteDB := func() *sqlitedb.SQLiteClient {
		mu.Lock()
		defer mu.Unlock()

		mem, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			panic(err)
		}
		t.Cleanup(func() { _ = mem.Close() })

		mem.SetMaxOpenConns(1)

		db, err := sqlitedb.NewSQLiteClientEx(mem, os.Stdout)
		if err != nil {
			panic(err)
		}
		if db == nil {
			panic("failed to create SQLite client")
		}

		sqlitedbtest.Apply(t, db)
		return db
	}()

	if len(sourceNames) > 0 {
		seedRateSources(t, sqliteDB, sourceNames...)
	}

	return sqliteDB
}

// seedRateSources inserts a minimal rate_source row for each provided name so
// dependent rows (rate_values, rate_user_subscriptions, rate_user_events) can
// reference them without violating the FK on rate_user_subscriptions.source_name.
// Tests that pick arbitrary source names (not from the canonical seed) should
// call this immediately after stubSQLiteDB.
func seedRateSources(t testing.TB, db *sqlitedb.SQLiteClient, names ...string) {
	t.Helper()
	r, err := NewRateSourceRepository(db)
	if err != nil {
		t.Fatalf("seedRateSources: NewRateSourceRepository: %v", err)
	}
	for _, name := range names {
		src := &domain.RateSource{
			Name:          name,
			Title:         "test fixture " + name,
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			URL:           "https://example.invalid/" + name,
			Interval:      "10m",
			Kind:          "BID",
			Active:        true,
		}
		if err := r.RetainRateSource(t.Context(), src); err != nil {
			t.Fatalf("seedRateSources(%q): %v", name, err)
		}
	}
}

// mockFailDB implements the db interface but always returns an error from
// Transaction and ReadOnlyTransaction. Use it to test error-handling branches
// that fire when the DB is unavailable.
type mockFailDB struct{ err error }

func (m *mockFailDB) Transaction(_ context.Context) (*sql.Tx, error) {
	return nil, errors.New(m.err.Error())
}

func (m *mockFailDB) ReadOnlyTransaction(_ context.Context) (*sql.Tx, error) {
	return nil, errors.New(m.err.Error())
}

var mu sync.Mutex
