package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	"github.com/seilbekskindirov/monitor/internal/repository"
	"github.com/seilbekskindirov/monitor/internal/ruledoctor"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// stubGenerator implements ruledoctor.Generator and returns a fixed Extraction.
type stubGenerator struct {
	extraction *ruledoctor.Extraction
	err        error
}

var _ ruledoctor.Generator = &stubGenerator{}

func (s *stubGenerator) Generate(_ context.Context, _ string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return `{"value":"450.75","css_selector":"td.rate","regex":"rate=([0-9.]+)","confidence":0.95}`, nil
}

func TestSqlQuote(t *testing.T) {
	t.Parallel()
	t.Run("plain string", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "'hello'", sqlQuote("hello"))
	})
	t.Run("string with single quote", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "'it''s'", sqlQuote("it's"))
	})
	t.Run("empty string", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "''", sqlQuote(""))
	})
}

func TestGenerateRuleID(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	id := generateRuleID(now)
	require.True(t, strings.HasPrefix(id, "R"), "ID must start with R")
	require.NotEmpty(t, id)
}

func TestArtifactPaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Now().UTC()
	sqlPath, reportPath, err := artifactPaths(dir, now)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(sqlPath, ".sql"))
	require.True(t, strings.HasSuffix(reportPath, ".report.txt"))
	require.True(t, strings.HasPrefix(filepath.Base(sqlPath), now.Format("20060102")))
}

func TestSha256Hex(t *testing.T) {
	t.Parallel()
	h1 := sha256Hex("hello")
	h2 := sha256Hex("hello")
	h3 := sha256Hex("world")
	require.Equal(t, h1, h2, "same input must produce same hash")
	require.NotEqual(t, h1, h3, "different input must produce different hash")
	require.Len(t, h1, 16, "8 bytes -> 16 hex chars")
}

func TestLoadTargets(t *testing.T) {
	t.Parallel()
	t.Run("valid file", func(t *testing.T) {
		t.Parallel()
		f, err := os.CreateTemp(t.TempDir(), "targets*.json")
		require.NoError(t, err)
		_, err = f.WriteString(`[{"target_kind":"rate","target_id":"nbk","source_url":"https://example.com","labels":["USD / KZT"]}]`)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		targets, err := loadTargets(f.Name())
		require.NoError(t, err)
		require.Len(t, targets, 1)
		require.Equal(t, "nbk", targets[0].TargetID)
	})
	t.Run("missing file returns clear error", func(t *testing.T) {
		t.Parallel()
		_, err := loadTargets("/no/such/file.json")
		require.Error(t, err)
		require.Contains(t, err.Error(), "targets file not found")
	})
	t.Run("invalid JSON returns error", func(t *testing.T) {
		t.Parallel()
		f, err := os.CreateTemp(t.TempDir(), "targets*.json")
		require.NoError(t, err)
		_, err = f.WriteString(`not json`)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		_, err = loadTargets(f.Name())
		require.Error(t, err)
	})
}

func TestFilterTargets(t *testing.T) {
	t.Parallel()
	all := []targetEntry{
		{TargetID: "a"},
		{TargetID: "b"},
		{TargetID: "c"},
	}
	t.Run("filters by ids", func(t *testing.T) {
		t.Parallel()
		out := filterTargets(all, []string{"a", "c"})
		require.Len(t, out, 2)
	})
	t.Run("no match returns empty", func(t *testing.T) {
		t.Parallel()
		out := filterTargets(all, []string{"x"})
		require.Empty(t, out)
	})
}

func TestGenerateRules(t *testing.T) {
	t.Parallel()

	t.Run("sql artifact format", func(t *testing.T) {
		t.Parallel()

		t.Run("verified rule produces valid SQL with BEGIN/COMMIT", func(t *testing.T) {
			t.Parallel()

			outDir := t.TempDir()
			now := time.Now().UTC()
			providerTag := "ruledoctor:groq:llama-3.1-8b-instant"

			sqlPath := filepath.Join(outDir, "test.sql")
			sqlF, err := os.Create(sqlPath)
			require.NoError(t, err)
			defer func() { _ = sqlF.Close() }()

			writeHeader(sqlF, now, providerTag, []string{"nbk_kzt"})
			_, err = sqlF.WriteString("\nBEGIN TRANSACTION;\n")
			require.NoError(t, err)

			results := []generateResult{
				{
					targetID: "nbk_kzt",
					label:    "USD / KZT",
					ruleID:   generateRuleID(now),
					rule: &domain.ExtractionRule{
						ID:          generateRuleID(now),
						TargetKind:  domain.ExtractionRuleKindRate,
						TargetID:    "nbk_kzt",
						Label:       "USD / KZT",
						SourceURL:   "https://nationalbank.kz/rates",
						Method:      domain.MethodCSS,
						Pattern:     `tr:has(td:contains("USD / KZT")) td:nth-child(4)`,
						ProviderTag: providerTag,
						ContextHash: sha256Hex("cleaned html"),
						Status:      domain.ExtractionRuleStatusActive,
						GeneratedAt: now,
						Notes:       "verified css=true regex=false",
					},
					ex: &ruledoctor.Extraction{
						Value:       "450.75",
						CSSSelector: `tr:has(td:contains("USD / KZT")) td:nth-child(4)`,
					},
					vr:      ruledoctor.VerifyResult{ValueMatches: true, CSSMatches: true},
					skipped: false,
				},
			}

			emitSQL(sqlF, results, now)
			_, err = sqlF.WriteString("\nCOMMIT;\n")
			require.NoError(t, err)
			require.NoError(t, sqlF.Close())

			content, err := os.ReadFile(sqlPath)
			require.NoError(t, err)
			sql := string(content)

			require.Contains(t, sql, "BEGIN TRANSACTION;")
			require.Contains(t, sql, "COMMIT;")
			require.Contains(t, sql, "UPDATE extraction_rules")
			require.Contains(t, sql, "SET status = 'superseded'")
			require.Contains(t, sql, "AND label = 'USD / KZT'")
			require.Contains(t, sql, "INSERT INTO extraction_rules")
			require.Contains(t, sql, "nbk_kzt")
			require.Contains(t, sql, "ruledoctor:groq:llama-3.1-8b-instant")
			require.NotContains(t, sql, "SKIPPED", "verified rule must not be skipped")
		})

		t.Run("verification failure produces skipped comment and no INSERT", func(t *testing.T) {
			t.Parallel()

			outDir := t.TempDir()
			now := time.Now().UTC()
			sqlPath := filepath.Join(outDir, "test.sql")
			sqlF, err := os.Create(sqlPath)
			require.NoError(t, err)
			defer func() { _ = sqlF.Close() }()

			writeHeader(sqlF, now, "groq:test", []string{"bcc_kzt"})
			_, err = sqlF.WriteString("\nBEGIN TRANSACTION;\n")
			require.NoError(t, err)

			results := []generateResult{
				{
					targetID: "bcc_kzt",
					label:    "USD / KZT",
					skipped:  true,
					reason:   "css matched no elements",
				},
			}
			emitSQL(sqlF, results, now)
			_, err = sqlF.WriteString("\nCOMMIT;\n")
			require.NoError(t, err)
			require.NoError(t, sqlF.Close())

			content, err := os.ReadFile(sqlPath)
			require.NoError(t, err)
			sql := string(content)

			require.Contains(t, sql, "Skipped")
			require.NotContains(t, sql, "INSERT INTO extraction_rules")
		})

		t.Run("single quotes in pattern are escaped", func(t *testing.T) {
			t.Parallel()
			require.Equal(t, "'it''s'", sqlQuote("it's"))
		})

		t.Run("multi-label target emits one UPDATE+INSERT pair per label", func(t *testing.T) {
			t.Parallel()

			now := time.Now().UTC()
			providerTag := "ruledoctor:groq:llama-3.1-8b-instant"

			results := []generateResult{
				{
					targetID: "nbk_kzt",
					label:    "EUR / KZT",
					rule: &domain.ExtractionRule{
						ID:          generateRuleID(now),
						TargetKind:  domain.ExtractionRuleKindRate,
						TargetID:    "nbk_kzt",
						Label:       "EUR / KZT",
						SourceURL:   "https://nationalbank.kz/rates",
						Method:      domain.MethodCSS,
						Pattern:     "td.eur",
						ProviderTag: providerTag,
						ContextHash: sha256Hex("ctx"),
						Status:      domain.ExtractionRuleStatusActive,
						GeneratedAt: now,
						Notes:       "verified css=true regex=true",
					},
					ex:      &ruledoctor.Extraction{Value: "542.16", CSSSelector: "td.eur"},
					vr:      ruledoctor.VerifyResult{ValueMatches: true, CSSMatches: true},
					skipped: false,
				},
				{
					targetID: "nbk_kzt",
					label:    "USD / KZT",
					rule: &domain.ExtractionRule{
						ID:          generateRuleID(now),
						TargetKind:  domain.ExtractionRuleKindRate,
						TargetID:    "nbk_kzt",
						Label:       "USD / KZT",
						SourceURL:   "https://nationalbank.kz/rates",
						Method:      domain.MethodCSS,
						Pattern:     "td.usd",
						ProviderTag: providerTag,
						ContextHash: sha256Hex("ctx"),
						Status:      domain.ExtractionRuleStatusActive,
						GeneratedAt: now,
						Notes:       "verified css=true regex=true",
					},
					ex:      &ruledoctor.Extraction{Value: "455.32", CSSSelector: "td.usd"},
					vr:      ruledoctor.VerifyResult{ValueMatches: true, CSSMatches: true},
					skipped: false,
				},
			}

			sqlPath := filepath.Join(t.TempDir(), "multi.sql")
			sqlF, err := os.Create(sqlPath)
			require.NoError(t, err)
			defer func() { _ = sqlF.Close() }()

			writeHeader(sqlF, now, providerTag, []string{"nbk_kzt"})
			_, err = sqlF.WriteString("\nBEGIN TRANSACTION;\n")
			require.NoError(t, err)
			emitSQL(sqlF, results, now)
			_, err = sqlF.WriteString("\nCOMMIT;\n")
			require.NoError(t, err)
			require.NoError(t, sqlF.Close())

			content, err := os.ReadFile(sqlPath)
			require.NoError(t, err)
			artifact := string(content)

			insertCount := strings.Count(artifact, "INSERT INTO extraction_rules")
			updateCount := strings.Count(artifact, "UPDATE extraction_rules")
			require.Equal(t, 2, insertCount, "must have exactly 2 INSERT lines")
			require.Equal(t, 2, updateCount, "must have exactly 2 UPDATE lines")
			require.Contains(t, artifact, "label = 'EUR / KZT'", "EUR label must appear in UPDATE")
			require.Contains(t, artifact, "label = 'USD / KZT'", "USD label must appear in UPDATE")

			// Apply the artifact to an in-memory SQLite and verify two rows exist.
			mem, sqlErr := sql.Open("sqlite", ":memory:")
			require.NoError(t, sqlErr)
			t.Cleanup(func() { _ = mem.Close() })
			mem.SetMaxOpenConns(1)

			sqliteClient, clientErr := sqlitedb.NewSQLiteClientEx(mem, os.Stdout)
			require.NoError(t, clientErr)

			repo, repoErr := repository.NewExtractionRuleRepository(sqliteClient)
			require.NoError(t, repoErr)

			// Apply the generated SQL artifact.
			_, execErr := mem.ExecContext(t.Context(), artifact)
			require.NoError(t, execErr, "artifact must apply cleanly against the real schema")

			active, fetchErr := repo.ObtainActiveRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "nbk_kzt")
			require.NoError(t, fetchErr)
			require.Len(t, active, 2, "two active rows with distinct labels must exist after apply")

			labels := make(map[string]bool, 2)
			for _, r := range active {
				labels[r.Label] = true
			}
			require.True(t, labels["EUR / KZT"])
			require.True(t, labels["USD / KZT"])
		})

		t.Run("single quote in pattern is safe in applied SQL", func(t *testing.T) {
			t.Parallel()

			now := time.Now().UTC()
			providerTag := "ruledoctor:groq:llama-3.1-8b-instant"

			// Pattern contains a single quote — must be escaped as ''.
			pattern := `td:contains("EUR ' KZT")`
			results := []generateResult{
				{
					targetID: "nbk_kzt",
					label:    "EUR / KZT",
					rule: &domain.ExtractionRule{
						ID:          generateRuleID(now),
						TargetKind:  domain.ExtractionRuleKindRate,
						TargetID:    "nbk_kzt",
						Label:       "EUR / KZT",
						SourceURL:   "https://nationalbank.kz/rates",
						Method:      domain.MethodCSS,
						Pattern:     pattern,
						ProviderTag: providerTag,
						ContextHash: sha256Hex("ctx"),
						Status:      domain.ExtractionRuleStatusActive,
						GeneratedAt: now,
						Notes:       "test",
					},
					ex:      &ruledoctor.Extraction{Value: "542.16", CSSSelector: pattern},
					vr:      ruledoctor.VerifyResult{ValueMatches: true, CSSMatches: true},
					skipped: false,
				},
			}

			sqlPath := filepath.Join(t.TempDir(), "quote.sql")
			sqlF, err := os.Create(sqlPath)
			require.NoError(t, err)
			defer func() { _ = sqlF.Close() }()

			writeHeader(sqlF, now, providerTag, []string{"nbk_kzt"})
			_, err = sqlF.WriteString("\nBEGIN TRANSACTION;\n")
			require.NoError(t, err)
			emitSQL(sqlF, results, now)
			_, err = sqlF.WriteString("\nCOMMIT;\n")
			require.NoError(t, err)
			require.NoError(t, sqlF.Close())

			content, err := os.ReadFile(sqlPath)
			require.NoError(t, err)

			mem, sqlErr := sql.Open("sqlite", ":memory:")
			require.NoError(t, sqlErr)
			t.Cleanup(func() { _ = mem.Close() })
			mem.SetMaxOpenConns(1)

			sqliteClient, clientErr := sqlitedb.NewSQLiteClientEx(mem, os.Stdout)
			require.NoError(t, clientErr)

			_, repoErr := repository.NewExtractionRuleRepository(sqliteClient)
			require.NoError(t, repoErr)

			_, execErr := mem.ExecContext(t.Context(), string(content))
			require.NoError(t, execErr, "artifact with single-quoted pattern must apply cleanly")
		})
	})
}
