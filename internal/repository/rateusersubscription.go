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
)

func NewRateUserSubscriptionRepository(db db) (*RateUserSubscriptionRepository, error) {
	r := &RateUserSubscriptionRepository{db: db}

	if m, err := sqlitedb.NewMigrator(db, r); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	} else if err = m.Run(context.Background()); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	return r, nil
}

type RateUserSubscriptionRepository struct {
	db db
}

func (r *RateUserSubscriptionRepository) Name() string { return rateUserSubscriptionTableName }

func (r *RateUserSubscriptionRepository) CheckUP(ctx context.Context) error {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	count, err := rateUserSubscriptionCount(tx, ctx, ";")
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

func (r *RateUserSubscriptionRepository) Migration() (map[string]string, error) {
	return map[string]string{
		rateUserSubscriptionTableName + "_001_table_initiate": `CREATE TABLE IF NOT EXISTS ` + rateUserSubscriptionTableName + ` (
	` + rateUserSubscriptionUserTypeFieldName + `            TEXT NOT NULL,
	` + rateUserSubscriptionUserIDFieldName + `              TEXT NOT NULL,
	` + rateUserSubscriptionSourceNameFieldName + `          TEXT NOT NULL,
 	` + rateUserSubscriptionConditionTypeFieldName + `       TEXT NOT NULL DEFAULT 'delta',
 	` + rateUserSubscriptionConditionValueFieldName + `      TEXT NOT NULL DEFAULT '10',
 	` + rateUserSubscriptionLatestNotifiedRateFieldName + `  REAL NOT NULL DEFAULT 0,
	` + rateUserSubscriptionUpdatedAtFieldName + `           TEXT NOT NULL,
	` + rateUserSubscriptionCreatedAtFieldName + `           TEXT NOT NULL,
	PRIMARY KEY (` + rateUserSubscriptionUserTypeFieldName + `, ` + rateUserSubscriptionUserIDFieldName + `, ` + rateUserSubscriptionSourceNameFieldName + `)
);
CREATE INDEX IF NOT EXISTS idx_` + rateUserSubscriptionTableName + `_userType ON ` + rateUserSubscriptionTableName + ` (` + rateUserSubscriptionUserTypeFieldName + `);
CREATE INDEX IF NOT EXISTS idx_` + rateUserSubscriptionTableName + `_userID ON ` + rateUserSubscriptionTableName + ` (` + rateUserSubscriptionUserIDFieldName + `);
CREATE INDEX IF NOT EXISTS idx_` + rateUserSubscriptionTableName + `_sourceName ON ` + rateUserSubscriptionTableName + ` (` + rateUserSubscriptionSourceNameFieldName + `);`,
		rateUserSubscriptionTableName + "_002_unique_source_user": `CREATE UNIQUE INDEX IF NOT EXISTS idx_` + rateUserSubscriptionTableName + `_sourceName_user ON ` + rateUserSubscriptionTableName + ` (` + rateUserSubscriptionSourceNameFieldName + `, ` + rateUserSubscriptionUserIDFieldName + `);`,
	}, nil
}

func (r *RateUserSubscriptionRepository) ObtainRateUserSubscriptionsByUserID(ctx context.Context, userType domain.UserType, userID string) ([]domain.RateUserSubscription, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	rows, err := rateUserSubscriptionQueryContext(tx, ctx, "WHERE "+rateUserSubscriptionUserTypeFieldName+" = ? AND "+rateUserSubscriptionUserIDFieldName+" = ?;", userType, userID)
	if err != nil {
		return nil, err
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	return rows, nil
}

func (r *RateUserSubscriptionRepository) ObtainRateUserSubscriptionsBySource(ctx context.Context, sourceName string) ([]domain.RateUserSubscription, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	rows, err := rateUserSubscriptionQueryContext(tx, ctx, "WHERE "+rateUserSubscriptionSourceNameFieldName+" = ?;", sourceName)
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

func (r *RateUserSubscriptionRepository) RetainRateUserSubscription(ctx context.Context, record *domain.RateUserSubscription) error {
	if record == nil {
		err := errors.New("user subscription is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	now := time.Now().UTC()

	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	count, err := rateUserSubscriptionCount(tx, ctx, "WHERE "+rateUserSubscriptionUserTypeFieldName+" = ? AND "+rateUserSubscriptionUserIDFieldName+" = ? AND "+rateUserSubscriptionSourceNameFieldName+" = ?;", record.UserType, record.UserID, record.SourceName)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	if count > 0 {
		cmd := "UPDATE" + " " + rateUserSubscriptionTableName + " SET " +
			rateUserSubscriptionConditionTypeFieldName + " = ?, " +
			rateUserSubscriptionConditionValueFieldName + " = ?, " +
			rateUserSubscriptionLatestNotifiedRateFieldName + " = ?, " +
			rateUserSubscriptionUpdatedAtFieldName + " = ? " +
			" WHERE " + rateUserSubscriptionUserTypeFieldName + " = ? AND " + rateUserSubscriptionUserIDFieldName + " = ? AND " + rateUserSubscriptionSourceNameFieldName + " = ?;"
		_, err = tx.ExecContext(
			ctx, cmd,
			record.ConditionType,
			record.ConditionValue,
			record.LatestNotifiedRate,
			record.UpdatedAt.Format(time.RFC3339),
			record.UserType,
			record.UserID,
			record.SourceName,
		)
	} else {
		cmd := "INSERT INTO" + " " + rateUserSubscriptionTableName +
			" (" +
			rateUserSubscriptionUserTypeFieldName + ", " +
			rateUserSubscriptionUserIDFieldName + ", " +
			rateUserSubscriptionSourceNameFieldName + ", " +
			rateUserSubscriptionConditionTypeFieldName + ", " +
			rateUserSubscriptionConditionValueFieldName + ", " +
			rateUserSubscriptionLatestNotifiedRateFieldName + ", " +
			rateUserSubscriptionUpdatedAtFieldName + ", " +
			rateUserSubscriptionCreatedAtFieldName +
			") VALUES (?, ?, ?, ?, ?, ?, ?, ?);"
		_, err = tx.ExecContext(
			ctx, cmd,
			record.UserType,
			record.UserID,
			record.SourceName,
			record.ConditionType,
			record.ConditionValue,
			record.LatestNotifiedRate,
			record.UpdatedAt.Format(time.RFC3339),
			record.CreatedAt.Format(time.RFC3339),
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

// SubscriptionSummary holds aggregated per-(source, user_type) notification statistics.
type SubscriptionSummary struct {
	SourceName        string
	UserType          domain.UserType
	SubscriptionCount int64
	LastSentAt        time.Time // zero if no events have been sent
	SuccessCount      int64
	FailedCount       int64
}

// ObtainSubscriptionSummaryBySource returns one row per (source_name, user_type) pair
// with aggregated subscription and event counts. user_id is never returned.
func (r *RateUserSubscriptionRepository) ObtainSubscriptionSummaryBySource(
	ctx context.Context, sourceName string,
) ([]SubscriptionSummary, error) {
	const query = "SELECT" + `
    s.source_name,
    s.user_type,
    COUNT(DISTINCT s.user_id)                                AS subscription_count,
    MAX(e.sent_at)                                           AS last_sent_at,
    SUM(CASE WHEN e.status='sent'   THEN 1 ELSE 0 END)      AS success_count,
    SUM(CASE WHEN e.status='failed' THEN 1 ELSE 0 END)      AS failed_count
FROM rate_user_subscriptions s
LEFT JOIN rate_user_events e
    ON e.source_name = s.source_name AND e.user_type = s.user_type
WHERE s.source_name = ?
GROUP BY s.source_name, s.user_type;`

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		return nil, errors.Join(err, internal.NewStackTraceError())
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	rows, err := tx.QueryContext(ctx, query, sourceName)
	if err != nil {
		return nil, errors.Join(err, internal.NewTraceError())
	}
	defer func() { err = errors.Join(err, rows.Close()) }()

	var result []SubscriptionSummary
	for rows.Next() {
		var s SubscriptionSummary
		var lastSentAt *string
		if scanErr := rows.Scan(
			&s.SourceName, &s.UserType,
			&s.SubscriptionCount, &lastSentAt,
			&s.SuccessCount, &s.FailedCount,
		); scanErr != nil {
			return nil, errors.Join(scanErr, internal.NewTraceError())
		}
		if lastSentAt != nil && *lastSentAt != "" {
			s.LastSentAt, _ = time.Parse(time.RFC3339, *lastSentAt)
		}
		result = append(result, s)
	}
	_ = tx.Rollback()
	return result, nil
}

func (r *RateUserSubscriptionRepository) RemoveRateUserSubscription(ctx context.Context, record *domain.RateUserSubscription) error {
	if record == nil {
		err := errors.New("user subscription is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	cmd := "DELETE FROM" + " " + rateUserSubscriptionTableName +
		" WHERE " + rateUserSubscriptionUserTypeFieldName + " = ?" +
		" AND " + rateUserSubscriptionUserIDFieldName + " = ?" +
		" AND " + rateUserSubscriptionSourceNameFieldName + " = ?;"
	_, err = tx.ExecContext(ctx, cmd, record.UserType, record.UserID, record.SourceName)
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
	rateUserSubscriptionTableName                   = "rate_user_subscriptions"
	rateUserSubscriptionUserTypeFieldName           = "user_type"
	rateUserSubscriptionUserIDFieldName             = "user_id"
	rateUserSubscriptionSourceNameFieldName         = "source_name"
	rateUserSubscriptionConditionTypeFieldName      = "condition_type"
	rateUserSubscriptionConditionValueFieldName     = "condition_value"
	rateUserSubscriptionLatestNotifiedRateFieldName = "latest_notified_rate"
	rateUserSubscriptionUpdatedAtFieldName          = "updated_at"
	rateUserSubscriptionCreatedAtFieldName          = "created_at"

	rateUserSubscriptionSqlSelect = "SELECT\n" +
		rateUserSubscriptionUserTypeFieldName + ", " +
		rateUserSubscriptionUserIDFieldName + ", " +
		rateUserSubscriptionSourceNameFieldName + ", " +
		rateUserSubscriptionConditionTypeFieldName + ", " +
		rateUserSubscriptionConditionValueFieldName + ", " +
		rateUserSubscriptionLatestNotifiedRateFieldName + ", " +
		rateUserSubscriptionUpdatedAtFieldName + ", " +
		rateUserSubscriptionCreatedAtFieldName +
		"\nFROM " + rateUserSubscriptionTableName
)

func rateUserSubscriptionCount(tx *sql.Tx, ctx context.Context, condition string, args ...any) (int64, error) {
	query := "SELECT\n" +
		" COUNT(*)\n" +
		"FROM " + rateUserSubscriptionTableName + "\n" + condition

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

func rateUserSubscriptionQueryContext(tx *sql.Tx, ctx context.Context, condition string, args ...any) (items []domain.RateUserSubscription, err error) {
	count, err := rateUserSubscriptionCount(tx, ctx, condition, args...)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return
	}
	if count == 0 {
		items = []domain.RateUserSubscription{}
		return
	}

	query := rateUserSubscriptionSqlSelect + "\n" + condition

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		err = errors.Join(err, fmt.Errorf("SQL: %s", query))
		err = errors.Join(err, internal.NewTraceError())
		return
	}
	defer func(rows io.Closer) { err = errors.Join(err, rows.Close()) }(rows)

	items = make([]domain.RateUserSubscription, 0, count)

	for rows.Next() {
		var item domain.RateUserSubscription
		var createdAt, updatedAt string

		err = rows.Scan(
			&item.UserType,
			&item.UserID,
			&item.SourceName,
			&item.ConditionType,
			&item.ConditionValue,
			&item.LatestNotifiedRate,
			&updatedAt,
			&createdAt,
		)
		if err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return
		}

		item.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return
		}

		item.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
		if err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		items = append(items, item)
	}

	return
}

func rateUserSubscriptionQueryRowContext(tx *sql.Tx, ctx context.Context, condition string, args ...any) (*domain.RateUserSubscription, error) {
	query := rateUserSubscriptionSqlSelect + "\n" + condition

	var item domain.RateUserSubscription
	var createdAt, updatedAt string
	err := tx.QueryRowContext(ctx, query, args...).Scan(
		&item.UserType,
		&item.UserID,
		&item.SourceName,
		&item.ConditionType,
		&item.ConditionValue,
		&item.LatestNotifiedRate,
		&updatedAt,
		&createdAt,
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
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	item.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	return &item, nil
}
