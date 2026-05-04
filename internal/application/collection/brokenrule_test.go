package collection

import (
	"context"
	"testing"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/stretchr/testify/require"
)

var _ brokenRuleRuleRepository = &mockBrokenRuleRepo{}
var _ brokenRuleHistoryRepository = &mockBrokenHistRepo{}

func TestBrokenRulePromoter_Run(t *testing.T) {
	t.Parallel()

	t.Run("three consecutive failures promote rule to broken", func(t *testing.T) {
		t.Parallel()

		rule := domain.ExtractionRule{
			ID:         "rule-abc",
			TargetID:   "src1",
			TargetKind: domain.ExtractionRuleKindRate,
			Status:     domain.ExtractionRuleStatusActive,
		}
		ruleRepo := &mockBrokenRuleRepo{rules: []domain.ExtractionRule{rule}}
		histRepo := &mockBrokenHistRepo{
			recordsBySource: map[string][]domain.ExecutionHistory{
				"src1": {
					{SourceName: "src1", Success: false, Timestamp: time.Now().Add(-3 * time.Minute)},
					{SourceName: "src1", Success: false, Timestamp: time.Now().Add(-2 * time.Minute)},
					{SourceName: "src1", Success: false, Timestamp: time.Now().Add(-1 * time.Minute)},
				},
			},
		}

		p := NewBrokenRulePromoter(ruleRepo, histRepo, nil)
		require.NoError(t, p.Run(t.Context()))
		require.Equal(t, domain.ExtractionRuleStatusBroken, ruleRepo.marked["rule-abc"])
	})
	t.Run("re-running promoter on already-broken rule is a no-op", func(t *testing.T) {
		t.Parallel()

		ruleRepo := &mockBrokenRuleRepo{rules: nil}
		histRepo := &mockBrokenHistRepo{}

		p := NewBrokenRulePromoter(ruleRepo, histRepo, nil)
		require.NoError(t, p.Run(t.Context()))
		require.Empty(t, ruleRepo.marked)
	})
	t.Run("two failures plus one success keeps rule active", func(t *testing.T) {
		t.Parallel()

		rule := domain.ExtractionRule{
			ID:         "rule-mixed",
			TargetID:   "src2",
			TargetKind: domain.ExtractionRuleKindRate,
			Status:     domain.ExtractionRuleStatusActive,
		}
		ruleRepo := &mockBrokenRuleRepo{rules: []domain.ExtractionRule{rule}}
		histRepo := &mockBrokenHistRepo{
			recordsBySource: map[string][]domain.ExecutionHistory{
				"src2": {
					{SourceName: "src2", Success: false},
					{SourceName: "src2", Success: true},
					{SourceName: "src2", Success: false},
				},
			},
		}

		p := NewBrokenRulePromoter(ruleRepo, histRepo, nil)
		require.NoError(t, p.Run(t.Context()))
		require.Empty(t, ruleRepo.marked, "mixed history must not promote to broken")
	})
	t.Run("failures but no active rule for source — no mark call", func(t *testing.T) {
		t.Parallel()

		ruleRepo := &mockBrokenRuleRepo{rules: nil}
		histRepo := &mockBrokenHistRepo{
			recordsBySource: map[string][]domain.ExecutionHistory{
				"src3": {
					{SourceName: "src3", Success: false},
					{SourceName: "src3", Success: false},
					{SourceName: "src3", Success: false},
				},
			},
		}

		p := NewBrokenRulePromoter(ruleRepo, histRepo, nil)
		require.NoError(t, p.Run(t.Context()))
		require.Empty(t, ruleRepo.marked)
	})
	t.Run("fewer than threshold history rows keeps rule active", func(t *testing.T) {
		t.Parallel()

		rule := domain.ExtractionRule{
			ID:         "rule-sparse",
			TargetID:   "src4",
			TargetKind: domain.ExtractionRuleKindRate,
			Status:     domain.ExtractionRuleStatusActive,
		}
		ruleRepo := &mockBrokenRuleRepo{rules: []domain.ExtractionRule{rule}}
		histRepo := &mockBrokenHistRepo{
			recordsBySource: map[string][]domain.ExecutionHistory{
				"src4": {
					{SourceName: "src4", Success: false},
					{SourceName: "src4", Success: false},
				},
			},
		}

		p := NewBrokenRulePromoter(ruleRepo, histRepo, nil)
		require.NoError(t, p.Run(t.Context()))
		require.Empty(t, ruleRepo.marked, "only 2 rows — not enough to promote")
	})
	t.Run("source with no history keeps rule active", func(t *testing.T) {
		t.Parallel()

		rule := domain.ExtractionRule{
			ID:         "rule-nohistory",
			TargetID:   "src5",
			TargetKind: domain.ExtractionRuleKindRate,
			Status:     domain.ExtractionRuleStatusActive,
		}
		ruleRepo := &mockBrokenRuleRepo{rules: []domain.ExtractionRule{rule}}
		histRepo := &mockBrokenHistRepo{}

		p := NewBrokenRulePromoter(ruleRepo, histRepo, nil)
		require.NoError(t, p.Run(t.Context()))
		require.Empty(t, ruleRepo.marked)
	})
}

type mockBrokenRuleRepo struct {
	rules  []domain.ExtractionRule
	marked map[string]domain.ExtractionRuleStatus
}

func (m *mockBrokenRuleRepo) ObtainAllActiveRules(_ context.Context, _ domain.ExtractionRuleKind) ([]domain.ExtractionRule, error) {
	return m.rules, nil
}

func (m *mockBrokenRuleRepo) MarkRuleStatus(_ context.Context, id string, status domain.ExtractionRuleStatus) error {
	if m.marked == nil {
		m.marked = make(map[string]domain.ExtractionRuleStatus)
	}
	m.marked[id] = status
	return nil
}

type mockBrokenHistRepo struct {
	recordsBySource map[string][]domain.ExecutionHistory
}

func (m *mockBrokenHistRepo) ObtainLastNExecutionHistoryBySourceName(_ context.Context, sourceName string, limit int64, _ bool) ([]domain.ExecutionHistory, error) {
	records := m.recordsBySource[sourceName]
	if int64(len(records)) > limit {
		records = records[:limit]
	}
	return records, nil
}
