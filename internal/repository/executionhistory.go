package repository

import (
	"context"
	"errors"
	"fmt"
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

	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM"+" "+executionHistoryTableName+";").Scan(&count); err != nil {
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

func (r *ExecutionHistoryRepository) RetainExecutionHistory(ctx context.Context, executionHistory *domain.ExecutionHistory) error {
	if executionHistory == nil {
		err := errors.New("execution history is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	if executionHistory.ID == "" {
		executionHistory.ID = generateExecutionHistoryID()
	}
	if executionHistory.Timestamp.IsZero() {
		executionHistory.Timestamp = time.Now().UTC()
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	var count int64
	err = tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM"+" "+executionHistoryTableName+" WHERE "+executionHistoryIDFieldName+" = ?;",
		executionHistory.ID,
	).Scan(&count)
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
			executionHistory.SourceName,
			executionHistory.Success,
			executionHistory.Error,
			executionHistory.Timestamp.Unix(),
			executionHistory.ID,
		)
	} else {
		cmd := "INSERT INTO" + " " + executionHistoryTableName +
			" (" + executionHistoryIDFieldName + ", " + executionHistorySourceNameFieldName + ", " + executionHistorySuccessFieldName + ", " + executionHistoryErrorFieldName + ", " + executionHistoryTimestampFieldName + ")" +
			" VALUES (?, ?, ?, ?, ?);"
		_, err = tx.ExecContext(
			ctx, cmd,
			executionHistory.ID,
			executionHistory.SourceName,
			executionHistory.Success,
			executionHistory.Error,
			executionHistory.Timestamp.Unix(),
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

// ObtainLastNExecutionHistoryBySourceName returns at most limit execution history records
// for the given source, ordered newest-first. When successOnly is true, only successful
// (success=1) rows are returned. Always returns a non-nil slice on success.
func (r *ExecutionHistoryRepository) ObtainLastNExecutionHistoryBySourceName(
	ctx context.Context,
	sourceName string,
	limit int,
	successOnly bool,
) ([]domain.ExecutionHistory, error) {
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

	cmd := "SELECT " + executionHistoryIDFieldName +
		", " + executionHistorySourceNameFieldName +
		", " + executionHistorySuccessFieldName +
		", " + executionHistoryErrorFieldName +
		", " + executionHistoryTimestampFieldName +
		" FROM " + executionHistoryTableName +
		" WHERE " + whereClause +
		" ORDER BY " + executionHistoryTimestampFieldName + " DESC LIMIT ?;"

	rows, err := tx.QueryContext(ctx, cmd, sourceName, limit)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	defer func(c interface{ Close() error }) { _ = c.Close() }(rows)

	records := make([]domain.ExecutionHistory, 0)
	for rows.Next() {
		var h domain.ExecutionHistory
		var ts int64
		if err = rows.Scan(&h.ID, &h.SourceName, &h.Success, &h.Error, &ts); err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}
		h.Timestamp = time.Unix(ts, 0).UTC()
		records = append(records, h)
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	return records, nil
}

func (r *ExecutionHistoryRepository) RemoveSourceExecutionHistory(ctx context.Context, executionHistory *domain.ExecutionHistory) error {
	if executionHistory == nil {
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
	_, err = tx.ExecContext(ctx, cmd, executionHistory.ID)
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

const (
	executionHistoryTableName           = "execution_history"
	executionHistoryIDFieldName         = "id"
	executionHistorySourceNameFieldName = "source_name"
	executionHistorySuccessFieldName    = "success"
	executionHistoryErrorFieldName      = "error"
	executionHistoryTimestampFieldName  = "timestamp"
)

func generateExecutionHistoryID() string {
	now := time.Now().UTC()
	return fmt.Sprintf("H%04d%02d%02d%02d%02d%02dZ%dT%X", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), uuid.NewV4().Bytes())
}
