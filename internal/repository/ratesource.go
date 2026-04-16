package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	"github.com/twinj/uuid"
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

	count, err := rateSourceCount(tx, ctx, ";")
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

func (r *RateSourceRepository) Migration() (map[string]string, error) {
	return map[string]string{
		rateSourceTableName + "_001_table_initiate": `CREATE TABLE IF NOT EXISTS ` + rateSourceTableName + ` (
	` + rateSourceNameFieldName + `          TEXT NOT NULL PRIMARY KEY,
	` + rateSourceTitleFieldName + `         TEXT NOT NULL,
	` + reteSourceBaseCurrencyFieldName + `  TEXT NOT NULL,
	` + reteSourceQuoteCurrencyFieldName + ` TEXT NOT NULL DEFAULT 'KZT',
	` + rateSourceURLFieldName + `           TEXT NOT NULL,
	` + reteSourceIntervalFieldName + `      TEXT NOT NULL DEFAULT '10m',
	` + rateSourceKindFieldName + `          TEXT NOT NULL,
	` + rateSourceActiveFieldName + `        INTEGER NOT NULL DEFAULT 1,
	` + rateSourceOptionsFieldName + `       TEXT NOT NULL DEFAULT '{}',
	` + rateSourceRulesFieldName + `         TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_` + rateSourceTableName + `_name ON ` + rateSourceTableName + ` (` + rateSourceNameFieldName + `);
CREATE INDEX IF NOT EXISTS idx_` + rateSourceTableName + `_currency ON ` + rateSourceTableName + ` (` + reteSourceBaseCurrencyFieldName + `,` + reteSourceBaseCurrencyFieldName + `);
CREATE INDEX IF NOT EXISTS idx_` + rateSourceTableName + `_kind ON ` + rateSourceTableName + ` (` + rateSourceKindFieldName + `);
CREATE INDEX IF NOT EXISTS idx_` + rateSourceTableName + `_active ON ` + rateSourceTableName + ` (` + rateSourceActiveFieldName + `);`,
	}, nil
}

func (r *RateSourceRepository) ObtainRateSourceByName(ctx context.Context, name string) (*domain.RateSource, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	rows, err := rateSourceQueryRowContext(tx, ctx, "WHERE "+rateSourceNameFieldName+" = ?;", name)
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

func (r *RateSourceRepository) ObtainAllRateSources(ctx context.Context) ([]domain.RateSource, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return nil, err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	rows, err := rateSourceQueryContext(tx, ctx, "ORDER BY "+rateSourceNameFieldName+" DESC;")
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

func (r *RateSourceRepository) RetainRateSource(ctx context.Context, record *domain.RateSource) error {
	if record == nil {
		err := errors.New("rate source is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	rules, err := json.Marshal(record.Rules)
	if err != nil {
		err = fmt.Errorf("marshal rules for rate source %q: %w", record.Name, err)
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	opts, err := json.Marshal(record.Options)
	if err != nil {
		err = fmt.Errorf("marshal options for rate source %q: %w", record.Name, err)
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}
	defer func(tx interface{ Rollback() error }) { _ = tx.Rollback() }(tx)

	count, err := rateSourceCount(tx, ctx, "WHERE "+rateSourceNameFieldName+" = ?;", record.Name)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	var res sql.Result
	if count > 0 {
		cmd := "UPDATE" + " " + rateSourceTableName + " SET " +
			rateSourceTitleFieldName + " = ?, " +
			reteSourceBaseCurrencyFieldName + " = ?, " +
			reteSourceQuoteCurrencyFieldName + " = ?, " +
			rateSourceURLFieldName + " = ?, " +
			reteSourceIntervalFieldName + " = ?, " +
			rateSourceKindFieldName + " = ?, " +
			rateSourceActiveFieldName + " = ?, " +
			rateSourceOptionsFieldName + " = ?, " +
			rateSourceRulesFieldName + " = ?" +
			" WHERE " + rateSourceNameFieldName + " = ?;"
		res, err = tx.ExecContext(
			ctx, cmd,
			record.Title,
			record.BaseCurrency,
			record.QuoteCurrency,
			record.URL,
			record.Interval,
			record.Kind,
			record.Active,
			string(opts),
			string(rules),
			record.Name,
		)
	} else {
		cmd := "INSERT INTO" + " " + rateSourceTableName +
			" (" +
			rateSourceNameFieldName + ", " +
			rateSourceTitleFieldName + ", " +
			reteSourceBaseCurrencyFieldName + ", " +
			reteSourceQuoteCurrencyFieldName + ", " +
			rateSourceURLFieldName + ", " +
			reteSourceIntervalFieldName + ", " +
			rateSourceKindFieldName + ", " +
			rateSourceActiveFieldName + ", " +
			rateSourceOptionsFieldName + ", " +
			rateSourceRulesFieldName +
			")" +
			" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);"
		res, err = tx.ExecContext(
			ctx, cmd,
			record.Name,
			record.Title,
			record.BaseCurrency,
			record.QuoteCurrency,
			record.URL,
			record.Interval,
			record.Kind,
			record.Active,
			string(opts),
			string(rules),
		)
	}
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}
	if rows <= 0 {
		err = errors.New("unexpected result: no rows affected")
		err = errors.Join(err, internal.ErrNotFound)
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	if err = tx.Commit(); err != nil {
		err = errors.Join(err, internal.NewStackTraceError())
		return err
	}

	return nil
}

func (r *RateSourceRepository) RemoveRateSource(ctx context.Context, record *domain.RateSource) error {
	if record == nil {
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
	_, err = tx.ExecContext(ctx, cmd, record.Name)
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
	rateSourceTableName              = "rate_sources"
	rateSourceNameFieldName          = "name"
	rateSourceTitleFieldName         = "title"
	rateSourceURLFieldName           = "url"
	reteSourceIntervalFieldName      = "interval"
	reteSourceBaseCurrencyFieldName  = "base_currency"
	reteSourceQuoteCurrencyFieldName = "quote_currency"
	rateSourceKindFieldName          = "kind"
	rateSourceActiveFieldName        = "active"
	rateSourceOptionsFieldName       = "options"
	rateSourceRulesFieldName         = "rules"

	rateSourceSqlSelect = "SELECT\n" +
		rateSourceNameFieldName + ", " +
		rateSourceTitleFieldName + ", " +
		reteSourceBaseCurrencyFieldName + ", " +
		reteSourceQuoteCurrencyFieldName + ", " +
		rateSourceURLFieldName + ", " +
		reteSourceIntervalFieldName + ", " +
		rateSourceKindFieldName + ", " +
		rateSourceActiveFieldName + ", " +
		rateSourceOptionsFieldName + ", " +
		rateSourceRulesFieldName + " " +
		"\nFROM " + rateSourceTableName
)

func generateRateSourceID() string {
	now := time.Now().UTC()
	return fmt.Sprintf("RS%04d%02d%02d%02d%02d%02dZ%dT%X", now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), uuid.NewV4().Bytes())
}

func rateSourceCount(tx *sql.Tx, ctx context.Context, condition string, args ...any) (int64, error) {
	query := "SELECT\n" +
		" COUNT(*)\n" +
		"FROM " + rateSourceTableName + "\n" + condition

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

func rateSourceQueryContext(tx *sql.Tx, ctx context.Context, condition string, args ...any) (items []domain.RateSource, err error) {
	count, err := rateSourceCount(tx, ctx, condition, args...)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return
	}
	if count == 0 {
		items = []domain.RateSource{}
		return
	}

	query := rateSourceSqlSelect + "\n" + condition

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		err = errors.Join(err, fmt.Errorf("SQL: %s", query))
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}
	defer func(rows io.Closer) { err = errors.Join(err, rows.Close()) }(rows)

	items = make([]domain.RateSource, 0, count)

	for rows.Next() {
		var optsJSON, rulesJSON string

		var item domain.RateSource
		if err = rows.Scan(
			&item.Name,
			&item.Title,
			&item.BaseCurrency,
			&item.QuoteCurrency,
			&item.URL,
			&item.Interval,
			&item.Kind,
			&item.Active,
			&optsJSON,
			&rulesJSON,
		); err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		if item.Kind != domain.RateSourceKindASK && item.Kind != domain.RateSourceKindBID {
			err = fmt.Errorf("unknown kind %s", item.Kind)
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		if err = json.Unmarshal([]byte(optsJSON), &item.Options); err != nil {
			err = fmt.Errorf("unmarshal options for source %q: %w", item.Name, err)
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		if err = json.Unmarshal([]byte(rulesJSON), &item.Rules); err != nil {
			err = fmt.Errorf("unmarshal rules for source %q: %w", item.Name, err)
			err = errors.Join(err, internal.NewTraceError())
			return nil, err
		}

		items = append(items, item)
	}

	return
}

func rateSourceQueryRowContext(tx *sql.Tx, ctx context.Context, condition string, args ...any) (*domain.RateSource, error) {
	query := rateSourceSqlSelect + "\n" + condition

	var item domain.RateSource
	var optionsJSON, rulesJSON string
	err := tx.QueryRowContext(ctx, query, args...).Scan(
		&item.Name,
		&item.Title,
		&item.BaseCurrency,
		&item.QuoteCurrency,
		&item.URL,
		&item.Interval,
		&item.Kind,
		&item.Active,
		&optionsJSON,
		&rulesJSON,
	)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		err = errors.Join(err, fmt.Errorf("SQL: %s", query))
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	if err = json.Unmarshal([]byte(optionsJSON), &item.Options); err != nil {
		err = fmt.Errorf("unmarshal options for source %q: %w", item.Name, err)
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	if err = json.Unmarshal([]byte(rulesJSON), &item.Rules); err != nil {
		err = fmt.Errorf("unmarshal rules for source %q: %w", item.Name, err)
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	return &item, nil
}
