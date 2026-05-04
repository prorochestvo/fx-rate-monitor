package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	"github.com/twinj/uuid"
)

func NewExtractionRuleRepository(db db) (*ExtractionRuleRepository, error) {
	r := &ExtractionRuleRepository{db: db}

	if m, err := sqlitedb.NewMigrator(db, r); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	} else if err = m.Run(context.Background()); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	return r, nil
}

type ExtractionRuleRepository struct {
	db db
}

func (r *ExtractionRuleRepository) Name() string { return extractionRuleTableName }

func (r *ExtractionRuleRepository) Migration() (map[string]string, error) {
	return map[string]string{
		"extraction_rules_001_table_initiate": `CREATE TABLE IF NOT EXISTS ` + extractionRuleTableName + ` (
	` + erFieldID + `               TEXT NOT NULL PRIMARY KEY,
	` + erFieldTargetKind + `       TEXT NOT NULL,
	` + erFieldTargetID + `         TEXT NOT NULL,
	` + erFieldLabel + `            TEXT NOT NULL DEFAULT '',
	` + erFieldSourceURL + `        TEXT NOT NULL,
	` + erFieldMethod + `           TEXT NOT NULL,
	` + erFieldPattern + `          TEXT NOT NULL,
	` + erFieldProviderTag + `      TEXT NOT NULL,
	` + erFieldContextHash + `      TEXT NOT NULL DEFAULT '',
	` + erFieldStatus + `           TEXT NOT NULL DEFAULT 'active',
	` + erFieldGeneratedAt + `      INT  NOT NULL,
	` + erFieldLastVerifiedAt + `   INT,
	` + erFieldNotes + `            TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_extraction_rules_active
	ON ` + extractionRuleTableName + ` (` + erFieldTargetKind + `, ` + erFieldTargetID + `, ` + erFieldStatus + `);
CREATE INDEX IF NOT EXISTS idx_extraction_rules_lookup
	ON ` + extractionRuleTableName + ` (` + erFieldTargetKind + `, ` + erFieldTargetID + `, ` + erFieldGeneratedAt + ` DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_extraction_rules_active_per_label
	ON ` + extractionRuleTableName + ` (` + erFieldTargetKind + `, ` + erFieldTargetID + `, ` + erFieldLabel + `) WHERE ` + erFieldStatus + ` = 'active';`,
	}, nil
}

// RetainExtractionRule inserts or updates a rule by ID (upsert by ID).
// Callers must ensure that at most one active rule exists per (kind, target)
// before inserting a new active one — use InstallActiveRule for that workflow.
func (r *ExtractionRuleRepository) RetainExtractionRule(ctx context.Context, record *domain.ExtractionRule) error {
	if record == nil {
		return errors.Join(errors.New("extraction rule is nil"), internal.NewTraceError())
	}

	if record.ID == "" {
		record.ID = generateExtractionRuleID()
	}
	if record.GeneratedAt.IsZero() {
		record.GeneratedAt = time.Now().UTC()
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		return errors.Join(err, internal.NewStackTraceError())
	}
	defer printRollbackError(tx)

	count, err := extractionRuleCount(tx, ctx, "WHERE "+erFieldID+" = ?;", record.ID)
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}

	var lastVerifiedAt sql.NullInt64
	if record.LastVerifiedAt != nil {
		lastVerifiedAt = sql.NullInt64{Valid: true, Int64: record.LastVerifiedAt.Unix()}
	}

	var res sql.Result
	if count > 0 {
		cmd := "UPDATE " + extractionRuleTableName + " SET " +
			erFieldTargetKind + " = ?, " +
			erFieldTargetID + " = ?, " +
			erFieldLabel + " = ?, " +
			erFieldSourceURL + " = ?, " +
			erFieldMethod + " = ?, " +
			erFieldPattern + " = ?, " +
			erFieldProviderTag + " = ?, " +
			erFieldContextHash + " = ?, " +
			erFieldStatus + " = ?, " +
			erFieldGeneratedAt + " = ?, " +
			erFieldLastVerifiedAt + " = ?, " +
			erFieldNotes + " = ?" +
			" WHERE " + erFieldID + " = ?;"
		res, err = tx.ExecContext(ctx, cmd,
			string(record.TargetKind),
			record.TargetID,
			record.Label,
			record.SourceURL,
			string(record.Method),
			record.Pattern,
			record.ProviderTag,
			record.ContextHash,
			string(record.Status),
			record.GeneratedAt.Unix(),
			lastVerifiedAt,
			record.Notes,
			record.ID,
		)
	} else {
		cmd := "INSERT INTO " + extractionRuleTableName + " (" +
			erFieldID + ", " +
			erFieldTargetKind + ", " +
			erFieldTargetID + ", " +
			erFieldLabel + ", " +
			erFieldSourceURL + ", " +
			erFieldMethod + ", " +
			erFieldPattern + ", " +
			erFieldProviderTag + ", " +
			erFieldContextHash + ", " +
			erFieldStatus + ", " +
			erFieldGeneratedAt + ", " +
			erFieldLastVerifiedAt + ", " +
			erFieldNotes +
			") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);"
		res, err = tx.ExecContext(ctx, cmd,
			record.ID,
			string(record.TargetKind),
			record.TargetID,
			record.Label,
			record.SourceURL,
			string(record.Method),
			record.Pattern,
			record.ProviderTag,
			record.ContextHash,
			string(record.Status),
			record.GeneratedAt.Unix(),
			lastVerifiedAt,
			record.Notes,
		)
	}
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}
	if rows <= 0 {
		return errors.Join(errors.New("unexpected result: no rows affected"),
			internal.ErrNotFound, internal.NewTraceError())
	}

	if err = tx.Commit(); err != nil {
		return errors.Join(err, internal.NewStackTraceError())
	}
	return nil
}

// InstallActiveRule supersedes any currently-active rule for the same
// (kind, target, label) and inserts rule as the new active one, all in one
// transaction. Multiple labels for the same (kind, target) coexist — only
// the prior occupant of the exact (kind, target, label) slot is superseded.
func (r *ExtractionRuleRepository) InstallActiveRule(ctx context.Context, rule *domain.ExtractionRule) error {
	if rule == nil {
		return errors.Join(errors.New("extraction rule is nil"), internal.NewTraceError())
	}
	if rule.ID == "" {
		rule.ID = generateExtractionRuleID()
	}
	if rule.GeneratedAt.IsZero() {
		rule.GeneratedAt = time.Now().UTC()
	}

	tx, err := r.db.Transaction(ctx)
	if err != nil {
		return errors.Join(err, internal.NewStackTraceError())
	}
	defer printRollbackError(tx)

	// Demote any currently-active rule for the same (kind, target, label) only.
	_, err = tx.ExecContext(ctx,
		"UPDATE "+extractionRuleTableName+
			" SET "+erFieldStatus+" = ?"+
			" WHERE "+erFieldTargetKind+" = ? AND "+erFieldTargetID+" = ? AND "+erFieldLabel+" = ? AND "+erFieldStatus+" = ?;",
		string(domain.ExtractionRuleStatusSuperseded),
		string(rule.TargetKind),
		rule.TargetID,
		rule.Label,
		string(domain.ExtractionRuleStatusActive),
	)
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}

	var lastVerifiedAt sql.NullInt64
	if rule.LastVerifiedAt != nil {
		lastVerifiedAt = sql.NullInt64{Valid: true, Int64: rule.LastVerifiedAt.Unix()}
	}

	cmd := "INSERT INTO " + extractionRuleTableName + " (" +
		erFieldID + ", " +
		erFieldTargetKind + ", " +
		erFieldTargetID + ", " +
		erFieldLabel + ", " +
		erFieldSourceURL + ", " +
		erFieldMethod + ", " +
		erFieldPattern + ", " +
		erFieldProviderTag + ", " +
		erFieldContextHash + ", " +
		erFieldStatus + ", " +
		erFieldGeneratedAt + ", " +
		erFieldLastVerifiedAt + ", " +
		erFieldNotes +
		") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);"
	res, err := tx.ExecContext(ctx, cmd,
		rule.ID,
		string(rule.TargetKind),
		rule.TargetID,
		rule.Label,
		rule.SourceURL,
		string(rule.Method),
		rule.Pattern,
		rule.ProviderTag,
		rule.ContextHash,
		string(domain.ExtractionRuleStatusActive),
		rule.GeneratedAt.Unix(),
		lastVerifiedAt,
		rule.Notes,
	)
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}
	if rows <= 0 {
		return errors.Join(errors.New("unexpected result: no rows affected"),
			internal.ErrNotFound, internal.NewTraceError())
	}

	if err = tx.Commit(); err != nil {
		return errors.Join(err, internal.NewStackTraceError())
	}
	return nil
}

// ObtainActiveRulesByTarget returns all active rules for (kind, targetID),
// ordered by label ASC. Returns an empty slice (not an error) when none exist.
// This is the hot path used by the collector; one source may have many
// per-pair labels (e.g. NBK exposes 39 currency pairs at one URL).
func (r *ExtractionRuleRepository) ObtainActiveRulesByTarget(ctx context.Context, kind domain.ExtractionRuleKind, targetID string) ([]domain.ExtractionRule, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		return nil, errors.Join(err, internal.NewStackTraceError())
	}
	defer printRollbackError(tx)

	rules, err := extractionRuleQueryContext(tx, ctx,
		"WHERE "+erFieldTargetKind+" = ? AND "+erFieldTargetID+" = ? AND "+erFieldStatus+" = ? ORDER BY "+erFieldLabel+" ASC;",
		string(kind), targetID, string(domain.ExtractionRuleStatusActive),
	)
	if err != nil {
		return nil, errors.Join(err, internal.NewTraceError())
	}

	if err = tx.Rollback(); err != nil {
		return nil, errors.Join(err, internal.NewStackTraceError())
	}
	return rules, nil
}

// ObtainActiveRuleByLabel returns the single active rule for the
// (kind, targetID, label) triple, or nil, nil if none exists.
// Used by InstallActiveRule to detect the prior occupant of a label slot.
func (r *ExtractionRuleRepository) ObtainActiveRuleByLabel(ctx context.Context, kind domain.ExtractionRuleKind, targetID, label string) (*domain.ExtractionRule, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		return nil, errors.Join(err, internal.NewStackTraceError())
	}
	defer printRollbackError(tx)

	rule, err := extractionRuleQueryRowContext(tx, ctx,
		"WHERE "+erFieldTargetKind+" = ? AND "+erFieldTargetID+" = ? AND "+erFieldLabel+" = ? AND "+erFieldStatus+" = ?;",
		string(kind), targetID, label, string(domain.ExtractionRuleStatusActive),
	)
	if err != nil {
		return nil, errors.Join(err, internal.NewTraceError())
	}

	if err = tx.Rollback(); err != nil {
		return nil, errors.Join(err, internal.NewStackTraceError())
	}
	return rule, nil
}

// ObtainAllRulesByTarget returns all rules for (kind, targetID) ordered newest-first.
func (r *ExtractionRuleRepository) ObtainAllRulesByTarget(ctx context.Context, kind domain.ExtractionRuleKind, targetID string) ([]domain.ExtractionRule, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		return nil, errors.Join(err, internal.NewStackTraceError())
	}
	defer printRollbackError(tx)

	rules, err := extractionRuleQueryContext(tx, ctx,
		"WHERE "+erFieldTargetKind+" = ? AND "+erFieldTargetID+" = ? ORDER BY "+erFieldGeneratedAt+" DESC;",
		string(kind), targetID,
	)
	if err != nil {
		return nil, errors.Join(err, internal.NewTraceError())
	}

	if err = tx.Rollback(); err != nil {
		return nil, errors.Join(err, internal.NewStackTraceError())
	}
	return rules, nil
}

// MarkRuleStatus transitions a rule to the given status.
func (r *ExtractionRuleRepository) MarkRuleStatus(ctx context.Context, id string, status domain.ExtractionRuleStatus) error {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		return errors.Join(err, internal.NewStackTraceError())
	}
	defer printRollbackError(tx)

	res, err := tx.ExecContext(ctx,
		"UPDATE "+extractionRuleTableName+" SET "+erFieldStatus+" = ? WHERE "+erFieldID+" = ?;",
		string(status), id,
	)
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}
	if rows <= 0 {
		return errors.Join(
			fmt.Errorf("extraction rule %q not found", id),
			internal.ErrNotFound,
			internal.NewTraceError(),
		)
	}

	if err = tx.Commit(); err != nil {
		return errors.Join(err, internal.NewStackTraceError())
	}
	return nil
}

// TouchVerifiedAt updates last_verified_at for the given rule ID.
// A failure is non-fatal for callers on the collection hot path.
func (r *ExtractionRuleRepository) TouchVerifiedAt(ctx context.Context, ruleID string, when time.Time) error {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		return errors.Join(err, internal.NewStackTraceError())
	}
	defer printRollbackError(tx)

	_, err = tx.ExecContext(ctx,
		"UPDATE "+extractionRuleTableName+" SET "+erFieldLastVerifiedAt+" = ? WHERE "+erFieldID+" = ?;",
		when.Unix(), ruleID,
	)
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}

	if err = tx.Commit(); err != nil {
		return errors.Join(err, internal.NewStackTraceError())
	}
	return nil
}

// ObtainBrokenTargets returns distinct target IDs that have at least one
// broken rule with no currently-active sibling for the SAME label. A target
// where every broken label has an active replacement is excluded.
func (r *ExtractionRuleRepository) ObtainBrokenTargets(ctx context.Context, kind domain.ExtractionRuleKind) ([]string, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		return nil, errors.Join(err, internal.NewStackTraceError())
	}
	defer printRollbackError(tx)

	// Per-label check: a (target, label) pair is "broken with no active
	// replacement" when a broken row exists and no active row shares the
	// same (kind, target, label) triple.
	query := "SELECT DISTINCT " + erFieldTargetID +
		" FROM " + extractionRuleTableName +
		" WHERE " + erFieldTargetKind + " = ?" +
		" AND " + erFieldStatus + " = ?" +
		" AND NOT EXISTS (" +
		"   SELECT 1 FROM " + extractionRuleTableName + " e2" +
		"   WHERE e2." + erFieldTargetKind + " = " + extractionRuleTableName + "." + erFieldTargetKind +
		"   AND e2." + erFieldTargetID + " = " + extractionRuleTableName + "." + erFieldTargetID +
		"   AND e2." + erFieldLabel + " = " + extractionRuleTableName + "." + erFieldLabel +
		"   AND e2." + erFieldStatus + " = ?" +
		" );"

	dbRows, err := tx.QueryContext(ctx, query,
		string(kind), string(domain.ExtractionRuleStatusBroken),
		string(domain.ExtractionRuleStatusActive),
	)
	if err != nil {
		return nil, errors.Join(err, internal.NewTraceError())
	}
	defer func() { err = errors.Join(err, dbRows.Close()) }()

	var targets []string
	for dbRows.Next() {
		var id string
		if scanErr := dbRows.Scan(&id); scanErr != nil {
			return nil, errors.Join(scanErr, internal.NewTraceError())
		}
		targets = append(targets, id)
	}

	if err = tx.Rollback(); err != nil {
		return nil, errors.Join(err, internal.NewStackTraceError())
	}

	return targets, nil
}

// ObtainAllActiveRules returns all rules with status='active' for the given kind.
// Used by the broken-rule promoter.
func (r *ExtractionRuleRepository) ObtainAllActiveRules(ctx context.Context, kind domain.ExtractionRuleKind) ([]domain.ExtractionRule, error) {
	tx, err := r.db.Transaction(ctx)
	if err != nil {
		return nil, errors.Join(err, internal.NewStackTraceError())
	}
	defer printRollbackError(tx)

	rules, err := extractionRuleQueryContext(tx, ctx,
		"WHERE "+erFieldTargetKind+" = ? AND "+erFieldStatus+" = ?;",
		string(kind), string(domain.ExtractionRuleStatusActive),
	)
	if err != nil {
		return nil, errors.Join(err, internal.NewTraceError())
	}

	if err = tx.Rollback(); err != nil {
		return nil, errors.Join(err, internal.NewStackTraceError())
	}
	return rules, nil
}

const (
	extractionRuleTableName = "extraction_rules"
	erFieldID               = "id"
	erFieldTargetKind       = "target_kind"
	erFieldTargetID         = "target_id"
	erFieldLabel            = "label"
	erFieldSourceURL        = "source_url"
	erFieldMethod           = "method"
	erFieldPattern          = "pattern"
	erFieldProviderTag      = "provider_tag"
	erFieldContextHash      = "context_hash"
	erFieldStatus           = "status"
	erFieldGeneratedAt      = "generated_at"
	erFieldLastVerifiedAt   = "last_verified_at"
	erFieldNotes            = "notes"

	erSQLSelect = "SELECT " +
		erFieldID + ", " +
		erFieldTargetKind + ", " +
		erFieldTargetID + ", " +
		erFieldLabel + ", " +
		erFieldSourceURL + ", " +
		erFieldMethod + ", " +
		erFieldPattern + ", " +
		erFieldProviderTag + ", " +
		erFieldContextHash + ", " +
		erFieldStatus + ", " +
		erFieldGeneratedAt + ", " +
		erFieldLastVerifiedAt + ", " +
		erFieldNotes +
		" FROM " + extractionRuleTableName
)

func generateExtractionRuleID() string {
	now := time.Now().UTC()
	return fmt.Sprintf("R%04d%02d%02d%02d%02d%02dZ%dT%X",
		now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(),
		now.Nanosecond(), uuid.NewV4().Bytes(),
	)
}

func extractionRuleCount(tx *sql.Tx, ctx context.Context, condition string, args ...any) (int64, error) {
	query := "SELECT COUNT(*) FROM " + extractionRuleTableName + " " + condition
	var count int64
	err := tx.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	} else if err != nil {
		return 0, errors.Join(err, fmt.Errorf("SQL: %s", query), internal.NewTraceError())
	}
	return count, nil
}

func scanExtractionRule(row interface {
	Scan(dest ...any) error
}) (*domain.ExtractionRule, error) {
	var (
		item           domain.ExtractionRule
		generatedAt    int64
		lastVerifiedAt sql.NullInt64
		kind           string
		method         string
		status         string
	)
	if err := row.Scan(
		&item.ID,
		&kind,
		&item.TargetID,
		&item.Label,
		&item.SourceURL,
		&method,
		&item.Pattern,
		&item.ProviderTag,
		&item.ContextHash,
		&status,
		&generatedAt,
		&lastVerifiedAt,
		&item.Notes,
	); err != nil {
		return nil, err
	}
	item.TargetKind = domain.ExtractionRuleKind(kind)
	item.Method = domain.Method(method)
	item.Status = domain.ExtractionRuleStatus(status)
	item.GeneratedAt = time.Unix(generatedAt, 0).UTC()
	if lastVerifiedAt.Valid {
		t := time.Unix(lastVerifiedAt.Int64, 0).UTC()
		item.LastVerifiedAt = &t
	}
	return &item, nil
}

func extractionRuleQueryRowContext(tx *sql.Tx, ctx context.Context, condition string, args ...any) (*domain.ExtractionRule, error) {
	query := erSQLSelect + " " + condition
	row := tx.QueryRowContext(ctx, query, args...)
	item, err := scanExtractionRule(row)
	if err != nil && errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, errors.Join(err, fmt.Errorf("SQL: %s", query), internal.NewTraceError())
	}
	return item, nil
}

func extractionRuleQueryContext(tx *sql.Tx, ctx context.Context, condition string, args ...any) ([]domain.ExtractionRule, error) {
	query := erSQLSelect + " " + condition
	dbRows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Join(err, fmt.Errorf("SQL: %s", query), internal.NewTraceError())
	}
	defer func() { err = errors.Join(err, dbRows.Close()) }()

	var items []domain.ExtractionRule
	for dbRows.Next() {
		item, scanErr := scanExtractionRule(dbRows)
		if scanErr != nil {
			return nil, errors.Join(scanErr, internal.NewTraceError())
		}
		items = append(items, *item)
	}
	if items == nil {
		items = []domain.ExtractionRule{}
	}
	return items, nil
}
