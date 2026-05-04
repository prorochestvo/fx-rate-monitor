package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/stretchr/testify/require"
)

var _ db = (*mockFailDB)(nil)

func newTestExtractionRuleRepo(t *testing.T) *ExtractionRuleRepository {
	t.Helper()
	sqliteDB := stubSQLiteDB(t)
	repo, err := NewExtractionRuleRepository(sqliteDB)
	require.NoError(t, err)
	require.NotNil(t, repo)
	return repo
}

func sampleRule(targetID, label string) *domain.ExtractionRule {
	return &domain.ExtractionRule{
		TargetKind:  domain.ExtractionRuleKindRate,
		TargetID:    targetID,
		Label:       label,
		SourceURL:   "https://example.com/rates",
		Method:      domain.MethodCSS,
		Pattern:     "tr:has(td:contains(\"USD / KZT\")) td:nth-child(4)",
		ProviderTag: "ruledoctor:groq:llama-3.1-8b-instant",
		ContextHash: "abc123",
		Status:      domain.ExtractionRuleStatusActive,
		Notes:       "test note",
	}
}

func TestNewExtractionRuleRepository(t *testing.T) {
	t.Parallel()
	t.Run("creates table on first boot", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		require.NotNil(t, repo)
	})
	t.Run("idempotent on already-migrated db", func(t *testing.T) {
		t.Parallel()
		sqliteDB := stubSQLiteDB(t)
		repo1, err := NewExtractionRuleRepository(sqliteDB)
		require.NoError(t, err)
		require.NotNil(t, repo1)
		repo2, err := NewExtractionRuleRepository(sqliteDB)
		require.NoError(t, err)
		require.NotNil(t, repo2)
	})
}

func TestExtractionRuleRepository_RetainExtractionRule(t *testing.T) {
	t.Parallel()
	t.Run("insert new rule", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		rule := sampleRule("nbk_kzt", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), rule))
		require.NotEmpty(t, rule.ID)
	})
	t.Run("upsert updates existing rule", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		rule := sampleRule("bcc_usd", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), rule))
		id := rule.ID
		rule.Pattern = "new_selector"
		rule.Notes = "updated"
		require.NoError(t, repo.RetainExtractionRule(t.Context(), rule))
		rules, err := repo.ObtainActiveRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "bcc_usd")
		require.NoError(t, err)
		require.Len(t, rules, 1)
		require.Equal(t, id, rules[0].ID)
		require.Equal(t, "new_selector", rules[0].Pattern)
		require.Equal(t, "updated", rules[0].Notes)
	})
	t.Run("round-trips nullable LastVerifiedAt and Label", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		rule := sampleRule("nbk_usd", "EUR / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), rule))
		rules, err := repo.ObtainActiveRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "nbk_usd")
		require.NoError(t, err)
		require.Len(t, rules, 1)
		require.Nil(t, rules[0].LastVerifiedAt)
		require.Equal(t, "EUR / KZT", rules[0].Label)

		now := time.Now().UTC().Truncate(time.Second)
		rule.LastVerifiedAt = &now
		require.NoError(t, repo.RetainExtractionRule(t.Context(), rule))
		rules2, err := repo.ObtainActiveRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "nbk_usd")
		require.NoError(t, err)
		require.Len(t, rules2, 1)
		require.NotNil(t, rules2[0].LastVerifiedAt)
		require.Equal(t, now.Unix(), rules2[0].LastVerifiedAt.Unix())
	})
	t.Run("nil rule returns error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		require.Error(t, repo.RetainExtractionRule(t.Context(), nil))
	})
	t.Run("db failure returns error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		repo.db = &mockFailDB{err: errors.New("db unavailable")}
		require.Error(t, repo.RetainExtractionRule(t.Context(), sampleRule("x", "y")))
	})
}

func TestExtractionRuleRepository_ObtainActiveRulesByTarget(t *testing.T) {
	t.Parallel()
	t.Run("returns empty slice when no rule exists", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		rules, err := repo.ObtainActiveRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "no_such_target")
		require.NoError(t, err)
		require.NotNil(t, rules)
		require.Empty(t, rules)
	})
	t.Run("returns active rule when one exists", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		inserted := sampleRule("nbk_eur", "EUR / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), inserted))
		rules, err := repo.ObtainActiveRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "nbk_eur")
		require.NoError(t, err)
		require.Len(t, rules, 1)
		require.Equal(t, inserted.ID, rules[0].ID)
		require.Equal(t, inserted.Pattern, rules[0].Pattern)
		require.Equal(t, domain.ExtractionRuleStatusActive, rules[0].Status)
	})
	t.Run("multiple labels returned in label ASC order", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		r1 := sampleRule("multi_label", "USD / KZT")
		r2 := sampleRule("multi_label", "EUR / KZT")
		r3 := sampleRule("multi_label", "RUB / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r1))
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r2))
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r3))
		rules, err := repo.ObtainActiveRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "multi_label")
		require.NoError(t, err)
		require.Len(t, rules, 3)
		// label ASC: EUR, RUB, USD
		require.Equal(t, "EUR / KZT", rules[0].Label)
		require.Equal(t, "RUB / KZT", rules[1].Label)
		require.Equal(t, "USD / KZT", rules[2].Label)
	})
	t.Run("does not return superseded rule", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		rule := sampleRule("nbk_rub", "RUB / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), rule))
		require.NoError(t, repo.MarkRuleStatus(t.Context(), rule.ID, domain.ExtractionRuleStatusSuperseded))
		rules, err := repo.ObtainActiveRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "nbk_rub")
		require.NoError(t, err)
		require.Empty(t, rules)
	})
	t.Run("db failure returns error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		repo.db = &mockFailDB{err: errors.New("db unavailable")}
		_, err := repo.ObtainActiveRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "x")
		require.Error(t, err)
	})
}

func TestExtractionRuleRepository_ObtainActiveRuleByLabel(t *testing.T) {
	t.Parallel()
	t.Run("returns nil nil when no rule exists", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		rule, err := repo.ObtainActiveRuleByLabel(t.Context(), domain.ExtractionRuleKindRate, "no_target", "no_label")
		require.NoError(t, err)
		require.Nil(t, rule)
	})
	t.Run("returns rule for exact (kind, target, label) triple", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		r1 := sampleRule("nbk", "EUR / KZT")
		r2 := sampleRule("nbk", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r1))
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r2))
		got, err := repo.ObtainActiveRuleByLabel(t.Context(), domain.ExtractionRuleKindRate, "nbk", "EUR / KZT")
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, r1.ID, got.ID)
	})
	t.Run("returns nil nil when label does not match", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		r := sampleRule("nbk", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r))
		got, err := repo.ObtainActiveRuleByLabel(t.Context(), domain.ExtractionRuleKindRate, "nbk", "EUR / KZT")
		require.NoError(t, err)
		require.Nil(t, got)
	})
	t.Run("db failure returns error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		repo.db = &mockFailDB{err: errors.New("db unavailable")}
		_, err := repo.ObtainActiveRuleByLabel(t.Context(), domain.ExtractionRuleKindRate, "x", "y")
		require.Error(t, err)
	})
}

func TestExtractionRuleRepository_ObtainAllRulesByTarget(t *testing.T) {
	t.Parallel()
	t.Run("returns all rules newest first", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		r1 := sampleRule("multi_target", "USD / KZT")
		r1.GeneratedAt = time.Now().Add(-2 * time.Minute).UTC()
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r1))
		require.NoError(t, repo.MarkRuleStatus(t.Context(), r1.ID, domain.ExtractionRuleStatusSuperseded))

		r2 := sampleRule("multi_target", "USD / KZT")
		r2.GeneratedAt = time.Now().UTC()
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r2))

		all, err := repo.ObtainAllRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "multi_target")
		require.NoError(t, err)
		require.Len(t, all, 2)
		require.Equal(t, r2.ID, all[0].ID, "newest first")
	})
	t.Run("returns empty slice when no rules", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		all, err := repo.ObtainAllRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "no_such")
		require.NoError(t, err)
		require.NotNil(t, all)
		require.Empty(t, all)
	})
	t.Run("db failure returns error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		repo.db = &mockFailDB{err: errors.New("db unavailable")}
		_, err := repo.ObtainAllRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "x")
		require.Error(t, err)
	})
}

func TestExtractionRuleRepository_MarkRuleStatus(t *testing.T) {
	t.Parallel()
	t.Run("transitions active to superseded", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		rule := sampleRule("transition_target", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), rule))
		require.NoError(t, repo.MarkRuleStatus(t.Context(), rule.ID, domain.ExtractionRuleStatusSuperseded))
		all, err := repo.ObtainAllRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "transition_target")
		require.NoError(t, err)
		require.Equal(t, domain.ExtractionRuleStatusSuperseded, all[0].Status)
	})
	t.Run("transitions active to broken", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		rule := sampleRule("broken_target", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), rule))
		require.NoError(t, repo.MarkRuleStatus(t.Context(), rule.ID, domain.ExtractionRuleStatusBroken))
		all, err := repo.ObtainAllRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "broken_target")
		require.NoError(t, err)
		require.Equal(t, domain.ExtractionRuleStatusBroken, all[0].Status)
	})
	t.Run("missing rule returns error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		require.Error(t, repo.MarkRuleStatus(t.Context(), "non_existent_id", domain.ExtractionRuleStatusBroken))
	})
	t.Run("db failure returns error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		repo.db = &mockFailDB{err: errors.New("db unavailable")}
		require.Error(t, repo.MarkRuleStatus(t.Context(), "any", domain.ExtractionRuleStatusBroken))
	})
}

func TestExtractionRuleRepository_InstallActiveRule(t *testing.T) {
	t.Parallel()
	t.Run("supersedes previous same-label rule and inserts new", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		r1 := sampleRule("install_target", "USD / KZT")
		r1.GeneratedAt = time.Now().Add(-time.Minute).UTC()
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r1))
		r2 := sampleRule("install_target", "USD / KZT")
		r2.GeneratedAt = time.Now().UTC()
		r2.Pattern = "new_css_selector"
		require.NoError(t, repo.InstallActiveRule(t.Context(), r2))

		active, err := repo.ObtainActiveRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "install_target")
		require.NoError(t, err)
		require.Len(t, active, 1)
		require.Equal(t, r2.ID, active[0].ID)
		require.Equal(t, "new_css_selector", active[0].Pattern)

		all, err := repo.ObtainAllRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "install_target")
		require.NoError(t, err)
		require.Len(t, all, 2)
		require.Equal(t, domain.ExtractionRuleStatusActive, all[0].Status)
		require.Equal(t, domain.ExtractionRuleStatusSuperseded, all[1].Status)
	})
	t.Run("different labels for same target both remain active", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		r1 := sampleRule("multi_label_install", "EUR / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r1))
		r2 := sampleRule("multi_label_install", "USD / KZT")
		require.NoError(t, repo.InstallActiveRule(t.Context(), r2))

		active, err := repo.ObtainActiveRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "multi_label_install")
		require.NoError(t, err)
		require.Len(t, active, 2, "both label slots must coexist as active")
	})
	t.Run("inserting second active row for same label returns sqlite uniqueness error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		r1 := sampleRule("constrained_target", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r1))

		r2 := sampleRule("constrained_target", "USD / KZT")
		require.Error(t, repo.RetainExtractionRule(t.Context(), r2),
			"second active row for same (kind, target, label) must fail")
	})
	t.Run("two active rows for same target but different labels coexist without error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		r1 := sampleRule("pair_target", "EUR / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r1))
		r2 := sampleRule("pair_target", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r2),
			"different labels must coexist in active state")
	})
	t.Run("nil rule returns error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		require.Error(t, repo.InstallActiveRule(t.Context(), nil))
	})
	t.Run("db failure returns error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		repo.db = &mockFailDB{err: errors.New("db unavailable")}
		require.Error(t, repo.InstallActiveRule(t.Context(), sampleRule("x", "y")))
	})
}

func TestExtractionRuleRepository_ObtainBrokenTargets(t *testing.T) {
	t.Parallel()
	t.Run("returns target with broken rule and no active rule for same label", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		rule := sampleRule("broken_source", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), rule))
		require.NoError(t, repo.MarkRuleStatus(t.Context(), rule.ID, domain.ExtractionRuleStatusBroken))

		targets, err := repo.ObtainBrokenTargets(t.Context(), domain.ExtractionRuleKindRate)
		require.NoError(t, err)
		require.Contains(t, targets, "broken_source")
	})
	t.Run("does not return target where broken label has active sibling for same label", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		old := sampleRule("mixed_source", "USD / KZT")
		old.GeneratedAt = time.Now().Add(-time.Hour).UTC()
		require.NoError(t, repo.RetainExtractionRule(t.Context(), old))
		require.NoError(t, repo.MarkRuleStatus(t.Context(), old.ID, domain.ExtractionRuleStatusBroken))

		// Install a fresh active rule for the same label — this should hide the target.
		newRule := sampleRule("mixed_source", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), newRule))

		targets, err := repo.ObtainBrokenTargets(t.Context(), domain.ExtractionRuleKindRate)
		require.NoError(t, err)
		require.NotContains(t, targets, "mixed_source")
	})
	t.Run("target with broken label and active different label is still broken", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		broken := sampleRule("partial_source", "EUR / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), broken))
		require.NoError(t, repo.MarkRuleStatus(t.Context(), broken.ID, domain.ExtractionRuleStatusBroken))

		// Active rule for a DIFFERENT label — must not suppress the broken one.
		active := sampleRule("partial_source", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), active))

		targets, err := repo.ObtainBrokenTargets(t.Context(), domain.ExtractionRuleKindRate)
		require.NoError(t, err)
		require.Contains(t, targets, "partial_source", "broken label with no active sibling of same label must appear")
	})
	t.Run("returns empty when no broken targets", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		targets, err := repo.ObtainBrokenTargets(t.Context(), domain.ExtractionRuleKindRate)
		require.NoError(t, err)
		require.Empty(t, targets)
	})
	t.Run("db failure returns error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		repo.db = &mockFailDB{err: errors.New("db unavailable")}
		_, err := repo.ObtainBrokenTargets(t.Context(), domain.ExtractionRuleKindRate)
		require.Error(t, err)
	})
}

func TestExtractionRuleRepository_TouchVerifiedAt(t *testing.T) {
	t.Parallel()
	t.Run("sets last_verified_at", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		rule := sampleRule("verify_target", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), rule))

		when := time.Now().UTC().Truncate(time.Second)
		require.NoError(t, repo.TouchVerifiedAt(t.Context(), rule.ID, when))

		rules, err := repo.ObtainActiveRulesByTarget(t.Context(), domain.ExtractionRuleKindRate, "verify_target")
		require.NoError(t, err)
		require.Len(t, rules, 1)
		require.NotNil(t, rules[0].LastVerifiedAt)
		require.Equal(t, when.Unix(), rules[0].LastVerifiedAt.Unix())
	})
	t.Run("db failure returns error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		repo.db = &mockFailDB{err: errors.New("db unavailable")}
		require.Error(t, repo.TouchVerifiedAt(t.Context(), "any", time.Now()))
	})
}

func TestExtractionRuleRepository_ObtainAllActiveRules(t *testing.T) {
	t.Parallel()
	t.Run("returns only active rules", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		r1 := sampleRule("active_target_a", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r1))
		r2 := sampleRule("active_target_b", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r2))
		r3 := sampleRule("broken_target_c", "USD / KZT")
		require.NoError(t, repo.RetainExtractionRule(t.Context(), r3))
		require.NoError(t, repo.MarkRuleStatus(t.Context(), r3.ID, domain.ExtractionRuleStatusBroken))

		all, err := repo.ObtainAllActiveRules(t.Context(), domain.ExtractionRuleKindRate)
		require.NoError(t, err)
		require.Len(t, all, 2)
		for _, r := range all {
			require.Equal(t, domain.ExtractionRuleStatusActive, r.Status)
		}
	})
	t.Run("db failure returns error", func(t *testing.T) {
		t.Parallel()
		repo := newTestExtractionRuleRepo(t)
		repo.db = &mockFailDB{err: errors.New("db unavailable")}
		_, err := repo.ObtainAllActiveRules(t.Context(), domain.ExtractionRuleKindRate)
		require.Error(t, err)
	})
}
