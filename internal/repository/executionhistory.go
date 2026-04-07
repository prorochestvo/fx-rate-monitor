package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	"github.com/twinj/uuid"
)

func NewExecutionHistoryRepository(db db) (*ExecutionHistoryRepository, error) {
	r := &ExecutionHistoryRepository{db: db}

	if m, err := sqlitedb.NewMigrator(db, r); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	} else if err = m.Run(context.Background()); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	return r, nil
}

type ExecutionHistoryRepository struct {
	db db
}

func (r *ExecutionHistoryRepository) Name() string { return executionHistoryTableName }

func (r *ExecutionHistoryRepository) CheckUP(ctx context.Context) error {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	count, err := executionHistoryCount(tx, ctx, ";")
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}

	if count < 0 {
		err = errors.New("unexpected result")
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}

	return nil
}

func (r *ExecutionHistoryRepository) Migration() (map[string]string, error) {
	return map[string]string{
		executionHistoryTableName + "_001_table_initiate": `CREATE TABLE IF NOT EXISTS ` + executionHistoryTableName + ` (
	` + executionHistoryIDFieldName + `          TEXT    NOT NULL PRIMARY KEY,
	` + executionHistorySourceNameFieldName + ` TEXT    NOT NULL,
	` + executionHistorySuccessFieldName + `    BOOLEAN NOT NULL,
	` + executionHistoryErrorFieldName + `      TEXT    NOT NULL DEFAULT '',
	` + executionHistoryTimestampFieldName + `  INT     NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_` + executionHistoryTableName + `_lookup_latest ON ` + executionHistoryTableName + ` (` + executionHistorySourceNameFieldName + `, ` + executionHistoryTimestampFieldName + ` DESC);
CREATE INDEX IF NOT EXISTS idx_` + executionHistoryTableName + `_lookup_errors ON ` + executionHistoryTableName + ` (` + executionHistorySourceNameFieldName + `, ` + executionHistorySuccessFieldName + `, ` + executionHistoryTimestampFieldName + ` DESC);`,
	}, nil
}

// ObtainLastNExecutionHistoryBySourceName returns at most limit execution history records
// for the given source, ordered newest-first. When successOnly is true, only successful
// (success=1) rows are returned. Always returns a non-nil slice on success.
func (r *ExecutionHistoryRepository) ObtainLastNExecutionHistoryBySourceName(ctx context.Context, sourceName string, limit int64, successOnly bool) ([]domain.ExecutionHistory, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	whereClause := executionHistorySourceNameFieldName + " = ?"
	if successOnly {
		whereClause += " AND " + executionHistorySuccessFieldName + " = 1"
	}

	rows, err := executionHistoryQueryContext(tx, ctx, "WHERE "+whereClause+" ORDER BY "+executionHistoryTimestampFieldName+" DESC LIMIT ?;", sourceName, limit)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	return rows, nil
}

func (r *ExecutionHistoryRepository) RetainExecutionHistory(ctx context.Context, record *domain.ExecutionHistory) error {
	if record == nil {
		err := errors.New("execution history is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	if record.ID == "" {
		record.ID = generateExecutionHistoryID()
	}
	if record.Timestamp.IsZero() {
		record.Timestamp = time.Now().UTC()
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	count, err := executionHistoryCount(tx, ctx, "WHERE "+executionHistoryIDFieldName+" = ?;", record.ID)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	if count > 0 {
		cmd := "UPDATE" + " " + executionHistoryTableName + " SET " +
			executionHistorySourceNameFieldName + " = ?, " +
			executionHistorySuccessFieldName + " = ?, " +
			executionHistoryErrorFieldName + " = ?, " +
			executionHistoryTimestampFieldName + " = ?" +
			" WHERE " + executionHistoryIDFieldName + " = ?;"
		_, err = tx.ExecContext(
			ctx, cmd,
			record.SourceName,
			record.Success,
			record.Error,
			record.Timestamp.Unix(),
			record.ID,
		)
	} else {
		cmd := "INSERT INTO" + " " + executionHistoryTableName +
			" (" +
			executionHistoryIDFieldName + ", " +
			executionHistorySourceNameFieldName + ", " +
			executionHistorySuccessFieldName + ", " +
			executionHistoryErrorFieldName + ", " +
			executionHistoryTimestampFieldName +
			")" +
			" VALUES (?, ?, ?, ?, ?);"
		_, err = tx.ExecContext(
			ctx, cmd,
			record.ID,
			record.SourceName,
			record.Success,
			record.Error,
			record.Timestamp.Unix(),
		)
	}
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	if err = tx.Commit(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}

	return nil
}

func (r *ExecutionHistoryRepository) RemoveSourceExecutionHistory(ctx context.Context, record *domain.ExecutionHistory) error {
	if record == nil {
		err := errors.New("execution history is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	cmd := "DELETE FROM" + " " + executionHistoryTableName + " WHERE " + executionHistoryIDFieldName + " = ?;"
	_, err = tx.ExecContext(ctx, cmd, record.ID)
	if err != nil {
		err = errors.Join(err, fmt.Errorf("SQL: %s", cmd))
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	if err = tx.Commit(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}

	return nil
}

const (
	executionHistoryTableName           = "execution_history"
	executionHistoryIDFieldName         = "id"
	executionHistorySourceNameFieldName = "source_name"
	executionHistorySuccessFieldName    = "success"
	executionHistoryErrorFieldName      = "error"
	executionHistoryTimestampFieldName  = "timestamp"

	executionHistorySqlSelect = "SELECT" + "\n" +
		executionHistoryIDFieldName + ", " +
		executionHistorySourceNameFieldName + ", " +
		executionHistorySuccessFieldName + ", " +
		executionHistoryErrorFieldName + ", " +
		executionHistoryTimestampFieldName +
		"\nFROM " + executionHistoryTableName
)

func generateExecutionHistoryID() string {
	now := time.Now().UTC()
	return fmt.Sprintf("H%04d%02d%02d%02d%02d%02dZ%dT%X", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), uuid.NewV4().Bytes())
}

func executionHistoryCount(tx *sql.Tx, ctx context.Context, condition string, args ...any) (int64, error) {
	query := "SELECT\n" +
		" COUNT(*)\n" +
		"FROM " + executionHistoryTableName + "\n" + condition

	var count int64
	err := tx.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	} else if err != nil {
		err = errors.Join(err, fmt.Errorf("SQL: %s", query))
		err = errors.Join(err, internal.NewTraceError())
		return 0, err
	}

	return count, nil
}

func executionHistoryQueryContext(tx *sql.Tx, ctx context.Context, condition string, args ...any) (items []domain.ExecutionHistory, err error) {
	count, err := executionHistoryCount(tx, ctx, condition, args...)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return
	}
	if count == 0 {
		items = []domain.ExecutionHistory{}
		return
	}

	query := executionHistorySqlSelect + "\n" + condition

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		err = errors.Join(err, fmt.Errorf("SQL: %s", query))
		err = errors.Join(err, internal.NewTraceError())
		return
	}
	defer func(rows io.Closer) { err = errors.Join(err, rows.Close()) }(rows)

	items = make([]domain.ExecutionHistory, 0, count)

	for rows.Next() {
		var item domain.ExecutionHistory
		var timestamp int64

		err = rows.Scan(
			&item.ID,
			&item.SourceName,
			&item.Success,
			&item.Error,
			&timestamp,
		)
		if err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return
		}

		item.Timestamp = time.Unix(timestamp, 0).UTC()

		items = append(items, item)
	}

	return
}

func executionHistoryQueryRowContext(tx *sql.Tx, ctx context.Context, condition string, args ...any) (*domain.ExecutionHistory, error) {
	query := executionHistorySqlSelect + "\n" + condition

	var item domain.ExecutionHistory
	var timestamp int64
	err := tx.QueryRowContext(ctx, query, args...).Scan(
		&item.ID,
		&item.SourceName,
		&item.Success,
		&item.Error,
		&timestamp,
	)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		err = errors.Join(err, fmt.Errorf("SQL: %s", query))
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	item.Timestamp = time.Unix(timestamp, 0).UTC()

	return &item, nil
}
