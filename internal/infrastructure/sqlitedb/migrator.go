package sqlitedb

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
)

// NewMigrator creates a Migrator for the given committer and sources.
func NewMigrator(db committer, sources ...source) (*Migrator, error) {
	tx, err := db.Transaction(context.Background())
	if err != nil || tx == nil {
		if err == nil {
			err = errors.New("transaction is nil")
		}
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err = tx.Exec(sqlCreateTable()); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	items, err := newDefaultMigrations()
	if err != nil {
		err = fmt.Errorf("load default migrations: %w", err)
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	for _, src := range sources {
		m, e := src.Migration()
		if e != nil {
			err = fmt.Errorf("load migrations: %w", e)
			err = errors.Join(err, internal.NewStackTraceError())
			return nil, err
		}

		if m == nil || len(m) == 0 {
			continue
		}

		for k, v := range m {
			items = append(items, migration{name: k, content: v})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].name < items[j].name
	})

	return &Migrator{db: db, items: items}, nil
}

// Migrator runs migrations from one or more migrationSource implementations
// using a committer (e.g. *SQLiteClient) to execute each statement in a transaction.
type Migrator struct {
	db    committer
	items []migration
}

// Run executes all pending migration statements from every source in order.
func (m *Migrator) Run(ctx context.Context) error {
	tx, err := m.db.Transaction(ctx)
	if err != nil || tx == nil {
		if err == nil {
			err = errors.New("transaction is nil")
		}
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for i, item := range m.items {
		if len(item.content) == 0 {
			continue
		}

		var exists bool
		if err = tx.QueryRow(sqlLookupFileName(item.name)).Scan(&exists); err != nil {
			err = fmt.Errorf("migrations[%d]: check of the %s is failed, reason: %s", i, item.name, err.Error())
			err = errors.Join(err, internal.NewStackTraceError())
			return err
		}
		if exists {
			continue
		}

		if _, err = tx.ExecContext(ctx, item.content); err != nil {
			err = fmt.Errorf("migrations[%d]: apply of the %s is failed, reason %s", i, item.name, err.Error())
			err = errors.Join(err, internal.NewTraceError())
			return err
		}

		if _, err = tx.ExecContext(ctx, sqlInsertFileName(item.name)); err != nil {
			err = fmt.Errorf("migrations[%d]: insert of the %s is failed, reason %s", i, item.name, err.Error())
			err = errors.Join(err, internal.NewTraceError())
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}

	return nil
}

type committer interface {
	Transaction(context.Context) (*sql.Tx, error)
}

type source interface {
	Migration() (map[string]string, error)
}

// migration is a sqlAction that executes a single SQL statement.
type migration struct {
	name    string
	content string
}

func newDefaultMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		err = fmt.Errorf("read migrations dir: %w", err)
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	items := make([]migration, 0, len(entries))

	for _, entry := range entries {
		fileName := entry.Name()

		if entry.IsDir() || !strings.HasSuffix(fileName, ".sql") {
			continue
		}

		var content []byte
		content, err = migrationsFS.ReadFile(path.Join("migrations", fileName))
		if err != nil {
			err = fmt.Errorf("read migration %s: %w", fileName, err)
			err = errors.Join(err, internal.NewStackTraceError())
			return nil, err
		}

		if content == nil || len(content) == 0 {
			continue
		}

		items = append(items, migration{
			name:    fileName,
			content: string(content),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].name < items[j].name
	})

	return items, nil
}

const (
	migrationTableName = "__schema_migrations"
)

func sqlCreateTable() string {
	return "CREATE TABLE IF NOT EXISTS" + " " + migrationTableName + " (filename TEXT PRIMARY KEY, applied_at TEXT NOT NULL);"
}

func sqlLookupFileName(fileName string) string {
	fileName = strings.ReplaceAll(fileName, "'", "''")
	return "SELECT COUNT(*) > 0 FROM" + " " + migrationTableName + " WHERE filename = '" + fileName + "';"
}

func sqlInsertFileName(fileName string) string {
	fileName = strings.ReplaceAll(fileName, "'", "''")
	now := strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339), "'", "''")
	return "INSERT INTO" + " " + migrationTableName + " (filename, applied_at) VALUES ('" + fileName + "', '" + now + "');"
}

//go:embed migrations/*.sql
var migrationsFS embed.FS
