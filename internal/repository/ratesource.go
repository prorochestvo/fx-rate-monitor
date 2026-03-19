package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
)

func NewRateSourceRepository(db db) (*RateSourceRepository, error) {
	r := &RateSourceRepository{db: db}

	if m, err := sqlitedb.NewMigrator(db, r); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	} else if err = m.Run(context.Background()); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	return r, nil
}

type RateSourceRepository struct {
	db db
}

func (r *RateSourceRepository) Name() string { return rateSourceTableName }

func (r *RateSourceRepository) CheckUP(ctx context.Context) error {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM"+" "+rateSourceTableName+";").Scan(&count); err != nil {
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

func (r *RateSourceRepository) Migration() (map[string]string, error) {
	return map[string]string{
		rateSourceTableName + "_001_table_initiate": `CREATE TABLE IF NOT EXISTS ` + rateSourceTableName + ` (
	` + rateSourceNameFieldName + `           TEXT NOT NULL PRIMARY KEY,
	` + rateSourceTitleFieldName + `          TEXT NOT NULL,
	` + reteSourceBaseCurrencyFieldName + `   TEXT NOT NULL,
	` + reteSourceQuoteCurrencyFieldName + `  TEXT NOT NULL,
	` + rateSourceURLFieldName + `            TEXT NOT NULL,
	` + reteSourceIntervalFieldName + `       TEXT NOT NULL DEFAULT '10m',
	` + rateSourceOptionsFieldName + `        TEXT NOT NULL DEFAULT '{}',
	` + rateSourceRulesFieldName + `          TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_` + rateSourceTableName + `_name ON ` + rateSourceTableName + ` (` + rateSourceNameFieldName + `);`,
	}, nil
}

func (r *RateSourceRepository) RetainRateSource(ctx context.Context, rateSource *domain.RateSource) error {
	if rateSource == nil {
		err := errors.New("rate source is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	rules, err := json.Marshal(rateSource.Rules)
	if err != nil {
		err = fmt.Errorf("marshal rules for rateSource %q: %w", rateSource.Name, err)
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	opts, err := json.Marshal(rateSource.Options)
	if err != nil {
		err = fmt.Errorf("marshal options for rateSource %q: %w", rateSource.Name, err)
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	var count int64
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM"+" "+rateSourceTableName+" WHERE "+rateSourceNameFieldName+" = ?;", rateSource.Name).Scan(&count)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	if count > 0 {
		cmd := "UPDATE" + " " + rateSourceTableName + " SET " +
			rateSourceTitleFieldName + " = ?, " +
			reteSourceBaseCurrencyFieldName + " = ?, " +
			reteSourceQuoteCurrencyFieldName + " = ?, " +
			rateSourceURLFieldName + " = ?, " +
			reteSourceIntervalFieldName + " = ?, " +
			rateSourceOptionsFieldName + " = ?, " +
			rateSourceRulesFieldName + " = ?" +
			" WHERE " + rateSourceNameFieldName + " = ?;"
		_, err = tx.ExecContext(
			ctx, cmd,
			rateSource.Title,
			rateSource.BaseCurrency,
			rateSource.QuoteCurrency,
			rateSource.URL,
			rateSource.Interval,
			string(opts),
			string(rules),
			rateSource.Name,
		)
	} else {
		cmd := "INSERT INTO" + " " + rateSourceTableName +
			" (" + rateSourceNameFieldName + ", " + rateSourceTitleFieldName + ", " + reteSourceBaseCurrencyFieldName + ", " + reteSourceQuoteCurrencyFieldName + ", " + rateSourceURLFieldName + ", " + reteSourceIntervalFieldName + ", " + rateSourceOptionsFieldName + ", " + rateSourceRulesFieldName + ")" +
			" VALUES (?, ?, ?, ?, ?, ?, ?, ?);"
		_, err = tx.ExecContext(
			ctx, cmd,
			rateSource.Name,
			rateSource.Title,
			rateSource.BaseCurrency,
			rateSource.QuoteCurrency,
			rateSource.URL,
			rateSource.Interval,
			string(opts),
			string(rules),
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

func (r *RateSourceRepository) RemoveRateSource(ctx context.Context, rateSource *domain.RateSource) error {
	if rateSource == nil {
		err := errors.New("rate source is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	cmd := "DELETE FROM" + " " + rateSourceTableName + " WHERE " + rateSourceNameFieldName + " = ?;"
	_, err = tx.ExecContext(ctx, cmd, rateSource.Name)
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

func (r *RateSourceRepository) ObtainRateSourceByName(ctx context.Context, name string) (*domain.RateSource, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	cmd := "SELECT " + rateSourceNameFieldName + ", " +
		rateSourceTitleFieldName + ", " +
		reteSourceBaseCurrencyFieldName + ", " +
		reteSourceQuoteCurrencyFieldName + ", " +
		rateSourceURLFieldName + ", " +
		reteSourceIntervalFieldName + ", " +
		rateSourceOptionsFieldName + ", " +
		rateSourceRulesFieldName + " " +
		"FROM " + rateSourceTableName + " " +
		"WHERE " + rateSourceNameFieldName + " = ?;"

	src, err := rateSourceQueryRowContext(tx, ctx, cmd, name)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	return src, nil
}

func (r *RateSourceRepository) ObtainAllRateSources(ctx context.Context) ([]domain.RateSource, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	cmd := "SELECT " + rateSourceNameFieldName + ", " +
		rateSourceTitleFieldName + ", " +
		reteSourceBaseCurrencyFieldName + ", " +
		reteSourceQuoteCurrencyFieldName + ", " +
		rateSourceURLFieldName + ", " +
		reteSourceIntervalFieldName + ", " +
		rateSourceOptionsFieldName + ", " +
		rateSourceRulesFieldName + " " +
		"FROM " + rateSourceTableName + " " +
		"ORDER BY " + rateSourceNameFieldName + " DESC;"

	src, err := rateSourceQueryContext(tx, ctx, cmd)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	if err = tx.Rollback(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}

	return src, nil
}

const (
	rateSourceTableName              = "rate_sources"
	rateSourceNameFieldName          = "name"
	rateSourceTitleFieldName         = "title"
	rateSourceURLFieldName           = "url"
	reteSourceIntervalFieldName      = "interval"
	reteSourceBaseCurrencyFieldName  = "base_currency"
	reteSourceQuoteCurrencyFieldName = "quote_currency"
	rateSourceOptionsFieldName       = "options"
	rateSourceRulesFieldName         = "rules"
)

//func generateRateSourceID() string {
//	now := time.Now().UTC()
//	return fmt.Sprintf("RS%04d%02d%02d%02d%02d%02dZ%dT%X", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), uuid.NewV4().Bytes())
//}

func rateSourceQueryContext(tx *sql.Tx, ctx context.Context, query string, args ...any) (items []domain.RateSource, err error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		err = errors.Join(err, fmt.Errorf("SQL: %s", query))
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	defer func(rows io.Closer) { err = errors.Join(err, rows.Close()) }(rows)

	items = make([]domain.RateSource, 0)

	for rows.Next() {
		var optsJSON, rulesJSON string

		var src domain.RateSource
		if err = rows.Scan(&src.Name, &src.Title, &src.BaseCurrency, &src.QuoteCurrency, &src.URL, &src.Interval, &optsJSON, &rulesJSON); err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		if err = json.Unmarshal([]byte(optsJSON), &src.Options); err != nil {
			err = fmt.Errorf("unmarshal options for source %q: %w", src.Name, err)
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		if err = json.Unmarshal([]byte(rulesJSON), &src.Rules); err != nil {
			err = fmt.Errorf("unmarshal rules for source %q: %w", src.Name, err)
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		items = append(items, src)
	}

	return
}

func rateSourceQueryRowContext(tx *sql.Tx, ctx context.Context, query string, args ...any) (*domain.RateSource, error) {
	var optsJSON, rulesJSON string

	var src domain.RateSource
	err := tx.QueryRowContext(ctx, query, args...).Scan(&src.Name, &src.Title, &src.BaseCurrency, &src.QuoteCurrency, &src.URL, &src.Interval, &optsJSON, &rulesJSON)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		err = errors.Join(err, fmt.Errorf("SQL: %s", query))
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	if err = json.Unmarshal([]byte(optsJSON), &src.Options); err != nil {
		err = fmt.Errorf("unmarshal options for source %q: %w", src.Name, err)
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	if err = json.Unmarshal([]byte(rulesJSON), &src.Rules); err != nil {
		err = fmt.Errorf("unmarshal rules for source %q: %w", src.Name, err)
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	return &src, nil
}
