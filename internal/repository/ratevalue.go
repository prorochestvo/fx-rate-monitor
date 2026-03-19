package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	"github.com/twinj/uuid"
)

func NewRateValueRepository(db db) (*RateValueRepository, error) {
	r := &RateValueRepository{db: db}

	if m, err := sqlitedb.NewMigrator(db, r); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	} else if err = m.Run(context.Background()); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	return r, nil
}

type RateValueRepository struct {
	db db
}

func (r *RateValueRepository) Name() string { return rateValueTableName }

func (r *RateValueRepository) CheckUP(ctx context.Context) error {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM"+" "+rateValueTableName+";").Scan(&count); err != nil {
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

func (r *RateValueRepository) Migration() (map[string]string, error) {
	return map[string]string{
		rateValueTableName + "_001_table_initiate": `CREATE TABLE IF NOT EXISTS ` + rateValueTableName + ` (
	` + rateValueIdFieldName + `             TEXT NOT NULL PRIMARY KEY,
	` + rateValueSourceNameFieldName + `     TEXT NOT NULL,
	` + rateValueBaseCurrencyFieldName + `   TEXT NOT NULL,
	` + rateValueQuoteCurrencyFieldName + `  TEXT NOT NULL,
	` + rateValuePriceFieldName + `          REAL NOT NULL,
	` + rateValueTimestampFieldName + `      TEXT NOT NULL
);
` + `CREATE INDEX IF NOT EXISTS idx_` + rateValueTableName + `_lookup ON ` + rateValueTableName + ` (` + rateValueSourceNameFieldName + `, ` + rateValueBaseCurrencyFieldName + `, ` + rateValueQuoteCurrencyFieldName + `, ` + rateValueTimestampFieldName + ` DESC);`,
	}, nil
}

func (r *RateValueRepository) ObtainAllRateValueBySourceName(ctx context.Context, sourceName string) ([]domain.RateValue, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	fields := []string{
		rateValueIdFieldName,
		rateValueSourceNameFieldName,
		rateValueBaseCurrencyFieldName,
		rateValueQuoteCurrencyFieldName,
		rateValuePriceFieldName,
		rateValueTimestampFieldName,
	}

	sqlCommand := "SELECT" + " " + strings.Join(fields, ", ") + " FROM " + rateValueTableName + " WHERE " + rateValueSourceNameFieldName + " = ?;"

	rows, err := tx.QueryContext(ctx, sqlCommand, sourceName)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	defer func(rows io.Closer) { _ = rows.Close() }(rows)

	var rates []domain.RateValue
	for rows.Next() {
		var rate domain.RateValue
		var timestamp string
		if err = rows.Scan(&rate.ID, &rate.SourceName, &rate.BaseCurrency, &rate.QuoteCurrency, &rate.Price, &timestamp); err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		rate.Timestamp, err = time.Parse(time.RFC3339, timestamp)
		if err != nil {
			err = fmt.Errorf("rate %s has invalid timestamp %s: %w", rate.ID, timestamp, err)
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		rates = append(rates, rate)
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	return rates, nil
}

func (r *RateValueRepository) ObtainLastNRateValuesBySourceName(ctx context.Context, sourceName string, limit int) ([]domain.RateValue, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	// Secondary sort by rowid DESC ensures deterministic newest-first ordering when
	// multiple rows share the same RFC3339 timestamp string (second precision).
	sqlCommand := "SELECT" + " " +
		rateValueIdFieldName + ", " +
		rateValueSourceNameFieldName + ", " +
		rateValueBaseCurrencyFieldName + ", " +
		rateValueQuoteCurrencyFieldName + ", " +
		rateValuePriceFieldName + ", " +
		rateValueTimestampFieldName +
		" FROM " + rateValueTableName +
		" WHERE " + rateValueSourceNameFieldName + " = ?" +
		" ORDER BY " + rateValueTimestampFieldName + " DESC, " + rateValueIdFieldName + " DESC LIMIT ?;"

	rows, err := tx.QueryContext(ctx, sqlCommand, sourceName, limit)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	defer func(rows io.Closer) { _ = rows.Close() }(rows)

	rates := make([]domain.RateValue, 0)
	for rows.Next() {
		var rate domain.RateValue
		var timestamp string
		if err = rows.Scan(&rate.ID, &rate.SourceName, &rate.BaseCurrency, &rate.QuoteCurrency, &rate.Price, &timestamp); err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		rate.Timestamp, err = time.Parse(time.RFC3339, timestamp)
		if err != nil {
			err = fmt.Errorf("rate %s has invalid timestamp %s: %w", rate.ID, timestamp, err)
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		rates = append(rates, rate)
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	return rates, nil
}

func (r *RateValueRepository) RetainRateValue(ctx context.Context, rateValue *domain.RateValue) error {
	if rateValue == nil {
		err := errors.New("rate value is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	if rateValue.ID == "" {
		rateValue.ID = generateRateValueID()
	}
	rateValue.Timestamp = time.Now().UTC()

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	cmd := "SELECT" + " COUNT(*) FROM " + rateValueTableName + " WHERE " + rateValueIdFieldName + " = ?;"
	var count int64
	err = tx.QueryRowContext(ctx, cmd, rateValue.ID).Scan(&count)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	fields := []string{
		rateValueSourceNameFieldName,
		rateValueBaseCurrencyFieldName,
		rateValueQuoteCurrencyFieldName,
		rateValuePriceFieldName,
		rateValueTimestampFieldName,
	}

	if count > 0 {
		cmd = "UPDATE" + " " + rateValueTableName + " SET " + strings.Join(fields, " = ?, ") + " = ? WHERE " + rateValueIdFieldName + " = ?;"
		_, err = tx.ExecContext(ctx, cmd, rateValue.SourceName, rateValue.BaseCurrency, rateValue.QuoteCurrency, rateValue.Price, rateValue.Timestamp.Format(time.RFC3339), rateValue.ID)
		if err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return err
		}
	} else {
		cmd = "INSERT INTO" + " " + rateValueTableName + " (" + rateValueIdFieldName + ", " + strings.Join(fields, ", ") + ") VALUES (?" + strings.Repeat(",?", len(fields)) + ");"
		_, err = tx.ExecContext(ctx, cmd, rateValue.ID, rateValue.SourceName, rateValue.BaseCurrency, rateValue.QuoteCurrency, rateValue.Price, rateValue.Timestamp.Format(time.RFC3339))
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

func (r *RateValueRepository) RemoveRateValue(ctx context.Context, rateValue *domain.RateValue) error {
	if rateValue == nil {
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

	cmd := "DELETE FROM" + " " + rateValueTableName + " WHERE id = ?;"
	_, err = tx.ExecContext(ctx, cmd, rateValue.ID)
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

type db interface {
	Transaction(ctx context.Context) (*sql.Tx, error)
}

const (
	rateValueTableName              = "rate_values"
	rateValueIdFieldName            = "id"
	rateValueSourceNameFieldName    = "source_name"
	rateValueBaseCurrencyFieldName  = "base_currency"
	rateValueQuoteCurrencyFieldName = "quote_currency"
	rateValuePriceFieldName         = "price"
	rateValueTimestampFieldName     = "timestamp"
)

func generateRateValueID() string {
	now := time.Now().UTC()
	return fmt.Sprintf("RV%04d%02d%02d%02d%02d%02dZ%dT%X", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), uuid.NewV4().Bytes())
}
