package collection

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/repository"
	"github.com/stretchr/testify/require"
)

var _ executionHistoryRepository = &repository.ExecutionHistoryRepository{}
var _ rateSourceRepository = &repository.RateSourceRepository{}
var _ rateValueRepository = &repository.RateValueRepository{}
var _ extractionRuleRepository = &mockExtractionRuleRepository{}
var _ extractionRuleRepository = &repository.ExtractionRuleRepository{}

func TestNewRateAgent(t *testing.T) {
	t.Parallel()

	t.Run("valid construction", func(t *testing.T) {
		t.Parallel()

		agent, err := NewRateAgent(
			"",
			&mockRateSourceRepository{},
			&mockExecutionHistoryRepository{},
			&mockRateValueRepository{},
			&mockExtractionRuleRepository{},
			io.Discard,
		)
		require.NoError(t, err)
		require.NotNil(t, agent)
	})
}

func TestRateAgent_execution(t *testing.T) {
	t.Parallel()

	t.Run("extractor succeeds — history retained as success", func(t *testing.T) {
		t.Parallel()

		histRepo := &mockExecutionHistoryRepository{}
		a := &RateAgent{
			rateExtractor:              &mockRateExtractor{},
			executionHistoryRepository: histRepo,
			extractionRuleRepository:   &mockExtractionRuleRepository{},
		}

		errs := a.execution(t.Context(), []domain.RateSource{{Name: "src1"}})
		require.Empty(t, errs)
		require.Len(t, histRepo.retained, 1)
		require.True(t, histRepo.retained[0].Success)
		require.Empty(t, histRepo.retained[0].Error)
		require.False(t, histRepo.retained[0].Timestamp.IsZero())
	})
	t.Run("extractor fails — history retained as failure", func(t *testing.T) {
		t.Parallel()

		histRepo := &mockExecutionHistoryRepository{}
		a := &RateAgent{
			rateExtractor:              &mockRateExtractor{err: errors.New("fetch error")},
			executionHistoryRepository: histRepo,
			extractionRuleRepository:   &mockExtractionRuleRepository{},
		}

		_ = a.execution(t.Context(), []domain.RateSource{{Name: "src1"}})
		require.Len(t, histRepo.retained, 1)
		require.False(t, histRepo.retained[0].Success)
		require.NotEmpty(t, histRepo.retained[0].Error)
	})
	t.Run("failing source appears in returned error map", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateExtractor:              &mockRateExtractor{err: errors.New("fetch error")},
			executionHistoryRepository: &mockExecutionHistoryRepository{},
			extractionRuleRepository:   &mockExtractionRuleRepository{},
		}

		errs := a.execution(t.Context(), []domain.RateSource{{Name: "src1"}})
		require.NotNil(t, errs["src1"])
	})
	t.Run("multiple sources each get their own history record", func(t *testing.T) {
		t.Parallel()

		histRepo := &mockExecutionHistoryRepository{}
		a := &RateAgent{
			rateExtractor:              &mockRateExtractor{},
			executionHistoryRepository: histRepo,
			extractionRuleRepository:   &mockExtractionRuleRepository{},
		}

		errs := a.execution(t.Context(), []domain.RateSource{{Name: "src1"}, {Name: "src2"}})
		require.Empty(t, errs)
		require.Len(t, histRepo.retained, 2)
	})
}

func TestRateAgent_Run(t *testing.T) {
	t.Parallel()

	t.Run("no sources — returns nil", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{sources: nil},
			executionHistoryRepository: &mockExecutionHistoryRepository{},
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              &mockRateExtractor{},
			extractionRuleRepository:   &mockExtractionRuleRepository{},
			logger:                     io.Discard,
		}

		require.NoError(t, a.Run(t.Context()))
	})
	t.Run("source not due — extractor never called", func(t *testing.T) {
		t.Parallel()

		extractor := &mockRateExtractor{}
		histRepo := &mockExecutionHistoryRepository{
			records: []domain.ExecutionHistory{{
				SourceName: "src1",
				Success:    true,
				Timestamp:  time.Now().UTC(),
			}},
		}
		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src1", Interval: "1h", Active: true}}},
			executionHistoryRepository: histRepo,
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              extractor,
			extractionRuleRepository:   &mockExtractionRuleRepository{},
			logger:                     io.Discard,
		}

		require.NoError(t, a.Run(t.Context()))
		require.Equal(t, 0, extractor.calls)
	})
	t.Run("source due — execution runs and history is retained", func(t *testing.T) {
		t.Parallel()

		histRepo := &mockExecutionHistoryRepository{records: nil}
		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src1", Interval: "1m", Title: "SRC", Active: true}}},
			executionHistoryRepository: histRepo,
			rateValueRepository:        &mockRateValueRepository{values: []domain.RateValue{{Price: 100}}},
			rateExtractor:              &mockRateExtractor{},
			extractionRuleRepository:   &mockExtractionRuleRepository{},
			logger:                     io.Discard,
		}

		require.NoError(t, a.Run(t.Context()))
		require.NotEmpty(t, histRepo.retained)
	})
	t.Run("invalid interval — error returned", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src1", Interval: "bad", Active: true}}},
			executionHistoryRepository: &mockExecutionHistoryRepository{},
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              &mockRateExtractor{},
			extractionRuleRepository:   &mockExtractionRuleRepository{},
			logger:                     io.Discard,
		}

		require.Error(t, a.Run(t.Context()))
	})
	t.Run("extractor error surfaced", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src1", Interval: "1m", Active: true}}},
			executionHistoryRepository: &mockExecutionHistoryRepository{},
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              &mockRateExtractor{err: errors.New("fetch error")},
			extractionRuleRepository:   &mockExtractionRuleRepository{},
			logger:                     io.Discard,
		}

		require.Error(t, a.Run(t.Context()))
	})
	t.Run("skips inactive source", func(t *testing.T) {
		t.Parallel()

		extractor := &mockRateExtractor{}
		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src1", Interval: "1m", Active: false}}},
			executionHistoryRepository: &mockExecutionHistoryRepository{},
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              extractor,
			extractionRuleRepository:   &mockExtractionRuleRepository{},
			logger:                     io.Discard,
		}

		require.NoError(t, a.Run(t.Context()))
		require.Equal(t, 0, extractor.calls, "inactive source must never be processed")
	})
	t.Run("source repo returns error", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{err: errors.New("db fail")},
			executionHistoryRepository: &mockExecutionHistoryRepository{},
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              &mockRateExtractor{},
			extractionRuleRepository:   &mockExtractionRuleRepository{},
			logger:                     io.Discard,
		}

		require.Error(t, a.Run(t.Context()))
	})
	t.Run("history repo error treats source as due", func(t *testing.T) {
		t.Parallel()

		extractor := &mockRateExtractor{}
		// obtainErr set -> ObtainLastNExecutionHistoryBySourceName returns error ->
		// isDue returns true -> source is treated as due and extractor is called.
		histRepo := &mockExecutionHistoryRepository{obtainErr: errors.New("hist fail")}
		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src1", Interval: "1h", Active: true}}},
			executionHistoryRepository: histRepo,
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              extractor,
			extractionRuleRepository:   &mockExtractionRuleRepository{},
			logger:                     io.Discard,
		}

		require.NoError(t, a.Run(t.Context()))
		require.Equal(t, 1, extractor.calls, "source must be treated as due when history fetch fails")
	})
}

func TestRateAgent_resolveRules(t *testing.T) {
	t.Parallel()

	t.Run("active extraction rule takes precedence over inline rules", func(t *testing.T) {
		t.Parallel()

		cssPattern := "tr:has(td:contains(\"USD\")) td:nth-child(4)"
		activeRule := domain.ExtractionRule{
			ID:      "rule-1",
			Label:   "USD / KZT",
			Method:  domain.MethodCSS,
			Pattern: cssPattern,
		}
		a := &RateAgent{
			extractionRuleRepository: &mockExtractionRuleRepository{rules: []domain.ExtractionRule{activeRule}},
		}
		source := domain.RateSource{
			Name:  "src_with_both",
			Rules: []domain.RateSourceRule{{Method: domain.MethodRegex, Pattern: `price=(\d+)`}},
		}

		rules, gotRules, err := a.resolveRules(t.Context(), source)
		require.NoError(t, err)
		require.Len(t, gotRules, 1)
		require.Equal(t, "rule-1", gotRules[0].ID)
		require.Len(t, rules, 1)
		require.Equal(t, domain.MethodCSS, rules[0].Method)
		require.Equal(t, cssPattern, rules[0].Pattern)
		require.Equal(t, "USD / KZT", rules[0].Pair, "label must be set as Pair")
	})
	t.Run("falls back to inline rules when no extraction rule exists", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			extractionRuleRepository: &mockExtractionRuleRepository{rules: nil},
		}
		inlineRules := []domain.RateSourceRule{{Method: domain.MethodRegex, Pattern: `price=(\d+)`}}
		source := domain.RateSource{Name: "legacy_src", Rules: inlineRules}

		rules, gotRules, err := a.resolveRules(t.Context(), source)
		require.NoError(t, err)
		require.Nil(t, gotRules)
		require.Equal(t, inlineRules, rules)
	})
	t.Run("extraction rule repo error propagates", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			extractionRuleRepository: &mockExtractionRuleRepository{err: errors.New("db fail")},
		}
		source := domain.RateSource{Name: "err_src"}

		_, _, err := a.resolveRules(t.Context(), source)
		require.Error(t, err)
	})
	t.Run("three active rules produce three rate source rules with correct pair labels", func(t *testing.T) {
		t.Parallel()

		active := []domain.ExtractionRule{
			{ID: "r1", Label: "EUR / KZT", Method: domain.MethodCSS, Pattern: "td.eur"},
			{ID: "r2", Label: "RUB / KZT", Method: domain.MethodCSS, Pattern: "td.rub"},
			{ID: "r3", Label: "USD / KZT", Method: domain.MethodCSS, Pattern: "td.usd"},
		}
		a := &RateAgent{
			extractionRuleRepository: &mockExtractionRuleRepository{rules: active},
		}
		source := domain.RateSource{Name: "nbk", Rules: []domain.RateSourceRule{{Method: domain.MethodRegex, Pattern: "old"}}}

		rules, gotRules, err := a.resolveRules(t.Context(), source)
		require.NoError(t, err)
		require.Len(t, gotRules, 3)
		require.Len(t, rules, 3)
		for i, r := range rules {
			require.Equal(t, active[i].Label, r.Pair)
			require.Equal(t, active[i].Pattern, r.Pattern)
		}
	})
	t.Run("successful run with active rules updates LastVerifiedAt for each", func(t *testing.T) {
		t.Parallel()

		active := []domain.ExtractionRule{
			{ID: "rule-verify-1", Label: "USD / KZT", Method: domain.MethodCSS, Pattern: "td.rate"},
			{ID: "rule-verify-2", Label: "EUR / KZT", Method: domain.MethodCSS, Pattern: "td.eur"},
		}
		ruleRepo := &mockExtractionRuleRepository{rules: active}
		histRepo := &mockExecutionHistoryRepository{}
		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src_verify", Interval: "1m", Active: true}}},
			executionHistoryRepository: histRepo,
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              &mockRateExtractor{},
			extractionRuleRepository:   ruleRepo,
			logger:                     io.Discard,
		}

		errs := a.execution(t.Context(), []domain.RateSource{{Name: "src_verify"}})
		require.Empty(t, errs)
		require.True(t, ruleRepo.touchedAt.After(time.Now().Add(-5*time.Second)),
			"LastVerifiedAt should be within 5s of now")
		require.Equal(t, 2, ruleRepo.touchCount, "both rules must get LastVerifiedAt updated")
	})
	t.Run("css selector matching no nodes records failure in history", func(t *testing.T) {
		t.Parallel()

		active := []domain.ExtractionRule{
			{ID: "rule-fail", Label: "USD / KZT", Method: domain.MethodCSS, Pattern: "td.nonexistent"},
		}
		ruleRepo := &mockExtractionRuleRepository{rules: active}
		histRepo := &mockExecutionHistoryRepository{}
		a := &RateAgent{
			executionHistoryRepository: histRepo,
			extractionRuleRepository:   ruleRepo,
			// extractor mock that returns an error simulating CSS no-match
			rateExtractor: &mockRateExtractor{err: errors.New("css selector matched no elements")},
		}

		errs := a.execution(t.Context(), []domain.RateSource{{Name: "src_fail"}})
		require.NotNil(t, errs["src_fail"])
		require.Len(t, histRepo.retained, 1)
		require.False(t, histRepo.retained[0].Success)
		require.Contains(t, histRepo.retained[0].Error, "css selector matched no elements")
	})
}

type mockRateSourceRepository struct {
	sources []domain.RateSource
	err     error
}

func (m *mockRateSourceRepository) ObtainRateSourceByName(_ context.Context, _ string) (*domain.RateSource, error) {
	return nil, m.err
}

func (m *mockRateSourceRepository) ObtainAllRateSources(_ context.Context) ([]domain.RateSource, error) {
	return m.sources, m.err
}

type mockExecutionHistoryRepository struct {
	records   []domain.ExecutionHistory
	retained  []*domain.ExecutionHistory
	err       error
	obtainErr error
}

func (m *mockExecutionHistoryRepository) RetainExecutionHistory(_ context.Context, h *domain.ExecutionHistory) error {
	cp := *h
	m.retained = append(m.retained, &cp)
	return m.err
}

func (m *mockExecutionHistoryRepository) ObtainLastNExecutionHistoryBySourceName(_ context.Context, _ string, _ int64, _ bool) ([]domain.ExecutionHistory, error) {
	if m.obtainErr != nil {
		return nil, m.obtainErr
	}
	return m.records, m.err
}

type mockRateValueRepository struct {
	values []domain.RateValue
	err    error
}

func (m *mockRateValueRepository) ObtainLastNRateValuesBySourceName(_ context.Context, _ string, _ int64) ([]domain.RateValue, error) {
	return m.values, m.err
}

func (m *mockRateValueRepository) RetainRateValue(_ context.Context, _ *domain.RateValue) error {
	return m.err
}

type mockRateExtractor struct {
	err   error
	calls int
}

func (m *mockRateExtractor) Run(_ context.Context, _ *domain.RateSource) error {
	m.calls++
	return m.err
}

type mockExtractionRuleRepository struct {
	rules      []domain.ExtractionRule
	err        error
	touchedAt  time.Time
	touchCount int
}

func (m *mockExtractionRuleRepository) ObtainActiveRulesByTarget(_ context.Context, _ domain.ExtractionRuleKind, _ string) ([]domain.ExtractionRule, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.rules, nil
}

func (m *mockExtractionRuleRepository) TouchVerifiedAt(_ context.Context, _ string, when time.Time) error {
	m.touchedAt = when
	m.touchCount++
	return nil
}
