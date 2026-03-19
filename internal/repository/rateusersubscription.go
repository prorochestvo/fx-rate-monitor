package repository

import (
	"context"
	"errors"
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

func (r *RateUserSubscriptionRepository) Name() string { return subscriptionTableName }

func (r *RateUserSubscriptionRepository) CheckUP(ctx context.Context) error {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM"+" "+subscriptionTableName+";").Scan(&count); err != nil {
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
		subscriptionTableName + "_001_table_initiate": `CREATE TABLE IF NOT EXISTS ` + subscriptionTableName + ` (
	` + subscriptionUserTypeFieldName + `        TEXT NOT NULL,
	` + subscriptionUserIDFieldName + `          TEXT NOT NULL,
	` + subscriptionSourceNameFieldName + `      TEXT NOT NULL,
 	` + subscriptionDeltaThresholdFieldName + `  REAL NOT NULL DEFAULT 0,
	` + subscriptionCreatedAtFieldName + `       TEXT NOT NULL,
	PRIMARY KEY (` + subscriptionUserTypeFieldName + `, ` + subscriptionUserIDFieldName + `, ` + subscriptionSourceNameFieldName + `)
);
CREATE INDEX IF NOT EXISTS idx_` + subscriptionTableName + `_userType ON ` + subscriptionTableName + ` (` + subscriptionUserTypeFieldName + `);
CREATE INDEX IF NOT EXISTS idx_` + subscriptionTableName + `_userID ON ` + subscriptionTableName + ` (` + subscriptionUserIDFieldName + `);
CREATE INDEX IF NOT EXISTS idx_` + subscriptionTableName + `_sourceName ON ` + subscriptionTableName + ` (` + subscriptionSourceNameFieldName + `);`,
		subscriptionTableName + "_002_unique_source_user": `CREATE UNIQUE INDEX IF NOT EXISTS idx_` + subscriptionTableName + `_sourceName_user ON ` + subscriptionTableName + ` (` + subscriptionSourceNameFieldName + `, ` + subscriptionUserIDFieldName + `);`,
	}, nil
}

func (r *RateUserSubscriptionRepository) RetainRateUserSubscription(ctx context.Context, userSubscription *domain.RateUserSubscription) error {
	if userSubscription == nil {
		err := errors.New("user subscription is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	if userSubscription.CreatedAt.IsZero() {
		userSubscription.CreatedAt = time.Now().UTC()
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	cmd := "INSERT OR IGNORE INTO" + " " + subscriptionTableName +
		" (" + subscriptionUserTypeFieldName + ", " + subscriptionUserIDFieldName + ", " +
		subscriptionSourceNameFieldName + ", " + subscriptionDeltaThresholdFieldName + ", " + subscriptionCreatedAtFieldName + ")" +
		" VALUES (?, ?, ?, ?, ?);"
	_, err = tx.ExecContext(ctx, cmd, userSubscription.UserType, userSubscription.UserID, userSubscription.Source, userSubscription.DeltaThreshold, userSubscription.CreatedAt.Format(time.RFC3339))
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

func (r *RateUserSubscriptionRepository) RemoveRateUserSubscription(ctx context.Context, userSubscription *domain.RateUserSubscription) error {
	if userSubscription == nil {
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

	cmd := "DELETE FROM" + " " + subscriptionTableName +
		" WHERE " + subscriptionUserTypeFieldName + " = ?" +
		" AND " + subscriptionUserIDFieldName + " = ?" +
		" AND " + subscriptionSourceNameFieldName + " = ?;"
	_, err = tx.ExecContext(ctx, cmd, userSubscription.UserType, userSubscription.UserID, userSubscription.Source)
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

func (r *RateUserSubscriptionRepository) ObtainRateUserSubscriptionsByUserID(ctx context.Context, userType domain.UserType, userID string) ([]domain.RateUserSubscription, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	cmd := "SELECT " + subscriptionUserTypeFieldName + ", " + subscriptionUserIDFieldName + ", " +
		subscriptionSourceNameFieldName + ", " + subscriptionDeltaThresholdFieldName + ", " + subscriptionCreatedAtFieldName +
		" FROM " + subscriptionTableName +
		" WHERE " + subscriptionUserTypeFieldName + " = ? AND " + subscriptionUserIDFieldName + " = ?;"

	rows, err := tx.QueryContext(ctx, cmd, userType, userID)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	defer func(rows io.Closer) { _ = rows.Close() }(rows)

	subs, err := scanSubscriptions(rows)
	if err != nil {
		return nil, err
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	return subs, nil
}

func (r *RateUserSubscriptionRepository) ObtainRateUserSubscriptionsBySource(ctx context.Context, sourceName string) ([]domain.RateUserSubscription, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	cmd := "SELECT " + subscriptionUserTypeFieldName + ", " + subscriptionUserIDFieldName + ", " +
		subscriptionSourceNameFieldName + ", " + subscriptionDeltaThresholdFieldName + ", " + subscriptionCreatedAtFieldName +
		" FROM " + subscriptionTableName +
		" WHERE " + subscriptionSourceNameFieldName + " = ?;"

	rows, err := tx.QueryContext(ctx, cmd, sourceName)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	defer func(rows io.Closer) { _ = rows.Close() }(rows)

	subs, err := scanSubscriptions(rows)
	if err != nil {
		return nil, err
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	return subs, nil
}

type subscriptionRows interface {
	Next() bool
	Scan(dest ...any) error
}

func scanSubscriptions(rows subscriptionRows) ([]domain.RateUserSubscription, error) {
	var subs []domain.RateUserSubscription
	for rows.Next() {
		var sub domain.RateUserSubscription
		var createdAt string
		if err := rows.Scan(&sub.UserType, &sub.UserID, &sub.Source, &sub.DeltaThreshold, &createdAt); err != nil {
			return nil, errors.Join(err, internal.NewTraceError())
		}
		var err error
		sub.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, errors.Join(err, internal.NewTraceError())
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

const (
	subscriptionTableName               = "rate_user_subscriptions"
	subscriptionUserTypeFieldName       = "user_type"
	subscriptionUserIDFieldName         = "user_id"
	subscriptionSourceNameFieldName     = "source_name"
	subscriptionDeltaThresholdFieldName = "delta_threshold"
	subscriptionCreatedAtFieldName      = "created_at"
)

//func generateRateUserSubscriptionID() string {
//	now := time.Now().UTC()
//	return fmt.Sprintf("US%04d%02d%02d%02d%02d%02dZ%dT%X", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), uuid.NewV4().Bytes())
//}
