package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	"github.com/twinj/uuid"
)

func NewRateUserEventRepository(db db) (*RateUserEventRepository, error) {
	r := &RateUserEventRepository{db: db}

	if m, err := sqlitedb.NewMigrator(db, r); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	} else if err = m.Run(context.Background()); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	return r, nil
}

type RateUserEventRepository struct {
	db db
}

func (r *RateUserEventRepository) Name() string { return rateUserEventTableName }

func (r *RateUserEventRepository) CheckUP(ctx context.Context) error {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	count, err := rateUserEventCount(tx, ctx, ";")
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

func (r *RateUserEventRepository) Migration() (map[string]string, error) {
	return map[string]string{
		rateUserEventTableName + "_001_table_initiate": `CREATE TABLE IF NOT EXISTS ` + rateUserEventTableName + ` (
	` + rateUserEventIDFieldName + `          TEXT NOT NULL PRIMARY KEY,
	` + rateUserEventUserTypeFieldName + `    TEXT NOT NULL,
	` + rateUserEventUserIDFieldName + `      TEXT NOT NULL,
	` + rateUserEventMessageFieldName + `     TEXT NOT NULL,
	` + rateUserEventStatusFieldName + `      TEXT NOT NULL DEFAULT '` + string(domain.RateUserEventStatusPending) + `',
	` + rateUserEventSentAtFieldName + `      TEXT,
	` + rateUserEventLastErrorFieldName + `   TEXT NOT NULL DEFAULT '',
	` + rateUserEventCreatedAtFieldName + `   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_` + rateUserEventTableName + `_status  ON ` + rateUserEventTableName + ` (` + rateUserEventStatusFieldName + `);
CREATE INDEX IF NOT EXISTS idx_` + rateUserEventTableName + `_user    ON ` + rateUserEventTableName + ` (` + rateUserEventUserTypeFieldName + `, ` + rateUserEventUserIDFieldName + `);
CREATE INDEX IF NOT EXISTS idx_` + rateUserEventTableName + `_created ON ` + rateUserEventTableName + ` (` + rateUserEventCreatedAtFieldName + ` DESC);
CREATE INDEX IF NOT EXISTS idx_` + rateUserEventTableName + `_failed ON ` + rateUserEventTableName + ` (` + rateUserEventCreatedAtFieldName + ` DESC) WHERE ` + rateUserEventStatusFieldName + ` = '` + string(domain.RateUserEventStatusFailed) + `';`,
	}, nil
}

func (r *RateUserEventRepository) ObtainLastNRateUserEvents(ctx context.Context, offset, limit int64, status ...domain.RateUserEventStatus) ([]domain.RateUserEvent, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	// whereClause is used for COUNT (no LIMIT/OFFSET — those must not be applied to COUNT).
	whereClause := ""
	var statusArgs []any
	if l := len(status); l > 0 {
		whereClause = fmt.Sprintf("WHERE %s in (%s)\n", rateUserEventStatusFieldName, strings.Repeat("?, ", l-1)+"?")
		for _, s := range status {
			statusArgs = append(statusArgs, s)
		}
	}

	count, err := rateUserEventCount(tx, ctx, whereClause+";", statusArgs...)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	if count == 0 {
		if err = tx.Rollback(); err != nil {
			err = errors.Join(err, internal.NewStackTraceError())
			return nil, err
		}
		return []domain.RateUserEvent{}, nil
	}

	// Build full condition with ORDER BY / LIMIT / OFFSET for the SELECT query.
	fullCondition := whereClause + "ORDER BY " + rateUserEventCreatedAtFieldName + " ASC\nLIMIT ?\nOFFSET ?;"
	selectArgs := append(statusArgs, limit, offset)

	query := rateUserEventSqlSelect + "\n" + fullCondition
	dbRows, err := tx.QueryContext(ctx, query, selectArgs...)
	if err != nil {
		err = errors.Join(err, fmt.Errorf("SQL: %s", query))
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	defer func() { err = errors.Join(err, dbRows.Close()) }()

	items := make([]domain.RateUserEvent, 0, count)
	for dbRows.Next() {
		var item domain.RateUserEvent
		var createdAt string
		var sentAt *string

		if scanErr := dbRows.Scan(
			&item.ID,
			&item.UserType,
			&item.UserID,
			&item.Message,
			&item.Status,
			&item.LastError,
			&createdAt,
			&sentAt,
		); scanErr != nil {
			return nil, errors.Join(scanErr, internal.NewTraceError())
		}

		item.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			err = fmt.Errorf("rate %s has invalid timestamp %s: %w", item.ID, createdAt, err)
			return nil, errors.Join(err, internal.NewTraceError())
		}

		if sentAt != nil && *sentAt != "" {
			item.SentAt, err = time.Parse(time.RFC3339, *sentAt)
			if err != nil {
				err = fmt.Errorf("rate %s has invalid timestamp %s: %w", item.ID, *sentAt, err)
				return nil, errors.Join(err, internal.NewTraceError())
			}
		}

		items = append(items, item)
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	return items, nil
}

func (r *RateUserEventRepository) ObtainUnprocessedRateUserEvents(ctx context.Context) ([]domain.RateUserEvent, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	rows, err := rateUserEventQueryContext(tx, ctx, "WHERE "+rateUserEventStatusFieldName+" in (?, ?) ORDER BY "+rateUserEventCreatedAtFieldName+" ASC;", domain.RateUserEventStatusPending, domain.RateUserEventStatusFailed)
	if err != nil {
		return nil, err
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	return rows, nil
}

func (r *RateUserEventRepository) ObtainRateUserEventById(ctx context.Context, id string) (*domain.RateUserEvent, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	row, err := rateUserEventQueryRowContext(tx, ctx, "WHERE "+rateUserEventIDFieldName+" = ? ORDER BY "+rateUserEventCreatedAtFieldName+" ASC;", id)
	if err != nil {
		return nil, err
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	return row, nil
}

func (r *RateUserEventRepository) RetainRateUserEvent(ctx context.Context, record *domain.RateUserEvent) error {
	if record == nil {
		err := errors.New("notification record is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	if record.ID == "" {
		record.ID = generateNotificationID()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if record.Status == "" {
		record.Status = domain.RateUserEventStatusPending
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	count, err := rateUserEventCount(tx, ctx, " WHERE "+rateValueIdFieldName+" = ?;", record.ID)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	var sentAt *string = nil
	if !record.SentAt.IsZero() {
		s := record.SentAt.Format(time.RFC3339)
		sentAt = &s
	}

	if count > 0 {
		cmd := "UPDATE" + " " + rateUserEventTableName + " SET " +
			rateUserEventUserTypeFieldName + " = ?, " +
			rateUserEventUserIDFieldName + " = ?, " +
			rateUserEventMessageFieldName + " = ?, " +
			rateUserEventStatusFieldName + " = ?, " +
			rateUserEventLastErrorFieldName + " = ?, " +
			rateUserEventSentAtFieldName + " = ?, " +
			rateUserEventCreatedAtFieldName + " = ? " +
			"WHERE " + rateUserEventIDFieldName + " = ?;"
		_, err = tx.ExecContext(ctx, cmd,
			record.UserType,
			record.UserID,
			record.Message,
			record.Status,
			record.LastError,
			sentAt,
			record.CreatedAt.Format(time.RFC3339),
			record.ID,
		)
		if err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return err
		}
	} else {
		cmd := "INSERT INTO" + " " + rateUserEventTableName + " (" +
			rateUserEventIDFieldName + ", " +
			rateUserEventUserTypeFieldName + ", " +
			rateUserEventUserIDFieldName + ", " +
			rateUserEventMessageFieldName + ", " +
			rateUserEventStatusFieldName + ", " +
			rateUserEventLastErrorFieldName + ", " +
			rateUserEventSentAtFieldName + ", " +
			rateUserEventCreatedAtFieldName +
			") VALUES (?, ?, ?, ?, ?, ?, ?, ?);"
		_, err = tx.ExecContext(ctx, cmd,
			record.ID,
			record.UserType,
			record.UserID,
			record.Message,
			record.Status,
			record.LastError,
			sentAt,
			record.CreatedAt.Format(time.RFC3339),
		)
		if err != nil {
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

func (r *RateUserEventRepository) RemoveRateUserEvent(ctx context.Context, record *domain.RateUserEvent) error {
	if record == nil {
		err := errors.New("rate value is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	cmd := "DELETE FROM" + " " + rateUserEventTableName + " WHERE " + rateUserEventIDFieldName + " = ?;"
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

func (r *RateUserEventRepository) RemoveRateUserEventOlderThan(ctx context.Context, duration time.Duration) error {
	if duration < 0 {
		duration = time.Duration(math.Abs(float64(duration)))
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	cmd := "DELETE FROM" + " " + rateUserEventTableName +
		" WHERE " + rateUserEventCreatedAtFieldName + " < ?" +
		" AND " + rateUserEventStatusFieldName + " != 'pending';"
	before := time.Now().UTC().Add(-duration)

	_, err = tx.ExecContext(ctx, cmd, before.Format(time.RFC3339))
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
	rateUserEventTableName          = "rate_user_events"
	rateUserEventIDFieldName        = "id"
	rateUserEventUserTypeFieldName  = "user_type"
	rateUserEventUserIDFieldName    = "user_id"
	rateUserEventMessageFieldName   = "message"
	rateUserEventStatusFieldName    = "status"
	rateUserEventLastErrorFieldName = "last_error"
	rateUserEventCreatedAtFieldName = "created_at"
	rateUserEventSentAtFieldName    = "sent_at"

	rateUserEventSqlSelect = "SELECT\n" +
		rateUserEventIDFieldName + ", " +
		rateUserEventUserTypeFieldName + ", " +
		rateUserEventUserIDFieldName + ", " +
		rateUserEventMessageFieldName + ", " +
		rateUserEventStatusFieldName + ", " +
		rateUserEventLastErrorFieldName + ", " +
		rateUserEventCreatedAtFieldName + ", " +
		rateUserEventSentAtFieldName +
		"\nFROM " + rateUserEventTableName
)

func generateNotificationID() string {
	now := time.Now().UTC()
	return fmt.Sprintf("RUE%04d%02d%02d%02d%02d%02dZ%dT%X", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), uuid.NewV4().Bytes())
}

func rateUserEventCount(tx *sql.Tx, ctx context.Context, condition string, args ...any) (int64, error) {
	query := "SELECT\n" +
		" COUNT(*)\n" +
		"FROM " + rateUserEventTableName + "\n" + condition

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

func rateUserEventQueryContext(tx *sql.Tx, ctx context.Context, condition string, args ...any) (items []domain.RateUserEvent, err error) {
	count, err := rateUserEventCount(tx, ctx, condition, args...)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return
	}
	if count == 0 {
		items = []domain.RateUserEvent{}
		return
	}

	query := rateUserEventSqlSelect + "\n" + condition

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		err = errors.Join(err, fmt.Errorf("SQL: %s", query))
		err = errors.Join(err, internal.NewTraceError())
		return
	}
	defer func(rows io.Closer) { err = errors.Join(err, rows.Close()) }(rows)

	items = make([]domain.RateUserEvent, 0, count)

	for rows.Next() {
		var item domain.RateUserEvent
		var createdAt string
		var sentAt *string

		err = rows.Scan(
			&item.ID,
			&item.UserType,
			&item.UserID,
			&item.Message,
			&item.Status,
			&item.LastError,
			&createdAt,
			&sentAt,
		)
		if err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return
		}

		item.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			err = fmt.Errorf("rate %s has invalid timestamp %s: %w", item.ID, createdAt, err)
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		if sentAt != nil && *sentAt != "" {
			item.SentAt, err = time.Parse(time.RFC3339, *sentAt)
			if err != nil {
				err = fmt.Errorf("rate %s has invalid timestamp %s: %w", item.ID, *sentAt, err)
				err = errors.Join(err, internal.NewTraceError())
				return nil, err
			}
		} else {
			item.SentAt = time.Time{}
		}

		items = append(items, item)
	}

	return
}

func rateUserEventQueryRowContext(tx *sql.Tx, ctx context.Context, condition string, args ...any) (*domain.RateUserEvent, error) {
	query := rateUserEventSqlSelect + "\n" + condition

	var item domain.RateUserEvent
	var createdAt string
	var sentAt *string
	err := tx.QueryRowContext(ctx, query, args...).Scan(
		&item.ID,
		&item.UserType,
		&item.UserID,
		&item.Message,
		&item.Status,
		&item.LastError,
		&createdAt,
		&sentAt,
	)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		err = errors.Join(err, fmt.Errorf("SQL: %s", query))
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	item.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		err = fmt.Errorf("rate %s has invalid timestamp %s: %w", item.ID, createdAt, err)
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	if sentAt != nil && *sentAt != "" {
		item.SentAt, err = time.Parse(time.RFC3339, *sentAt)
		if err != nil {
			err = fmt.Errorf("rate %s has invalid timestamp %s: %w", item.ID, *sentAt, err)
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}
	} else {
		item.SentAt = time.Time{}
	}

	return &item, nil
}
