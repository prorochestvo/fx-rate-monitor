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

func TestNewRateAgent(t *testing.T) {
	t.Parallel()

	t.Run("valid construction", func(t *testing.T) {
		t.Parallel()

		agent, err := NewRateAgent(
			"",
			&mockRateSourceRepository{},
			&mockExecutionHistoryRepository{},
			&mockRateValueRepository{},
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
			rateSourceRepository:       &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src1", Interval: "1h"}}},
			executionHistoryRepository: histRepo,
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              extractor,
			logger:                     io.Discard,
		}

		require.NoError(t, a.Run(t.Context()))
		require.Equal(t, 0, extractor.calls)
	})
	t.Run("source due — execution runs and history is retained", func(t *testing.T) {
		t.Parallel()

		histRepo := &mockExecutionHistoryRepository{records: nil}
		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src1", Interval: "1m", Title: "SRC"}}},
			executionHistoryRepository: histRepo,
			rateValueRepository:        &mockRateValueRepository{values: []domain.RateValue{{Price: 100}}},
			rateExtractor:              &mockRateExtractor{},
			logger:                     io.Discard,
		}

		require.NoError(t, a.Run(t.Context()))
		require.NotEmpty(t, histRepo.retained)
	})
	t.Run("invalid interval — error returned", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src1", Interval: "bad"}}},
			executionHistoryRepository: &mockExecutionHistoryRepository{},
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              &mockRateExtractor{},
			logger:                     io.Discard,
		}

		require.Error(t, a.Run(t.Context()))
	})
	t.Run("extractor error surfaced", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src1", Interval: "1m"}}},
			executionHistoryRepository: &mockExecutionHistoryRepository{},
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              &mockRateExtractor{err: errors.New("fetch error")},
			logger:                     io.Discard,
		}

		require.Error(t, a.Run(t.Context()))
	})
	t.Run("source repo returns error", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{err: errors.New("db fail")},
			executionHistoryRepository: &mockExecutionHistoryRepository{},
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              &mockRateExtractor{},
			logger:                     io.Discard,
		}

		require.Error(t, a.Run(t.Context()))
	})
	t.Run("history repo error treats source as due", func(t *testing.T) {
		t.Parallel()

		extractor := &mockRateExtractor{}
		// obtainErr set → ObtainLastNExecutionHistoryBySourceName returns error →
		// isDue returns true → source is treated as due and extractor is called.
		histRepo := &mockExecutionHistoryRepository{obtainErr: errors.New("hist fail")}
		a := &RateAgent{
			rateSourceRepository:       &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src1", Interval: "1h"}}},
			executionHistoryRepository: histRepo,
			rateValueRepository:        &mockRateValueRepository{},
			rateExtractor:              extractor,
			logger:                     io.Discard,
		}

		require.NoError(t, a.Run(t.Context()))
		require.Equal(t, 1, extractor.calls, "source must be treated as due when history fetch fails")
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
