package api

import (
	"context"
	"errors"
	"testing"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/repository"
	"github.com/stretchr/testify/require"
)

var _ executionHistoryRepository = &repository.ExecutionHistoryRepository{}
var _ rateSourceRepository = &repository.RateSourceRepository{}
var _ rateValueRepository = &repository.RateValueRepository{}
var _ rateUserSubscriptionRepository = &repository.RateUserSubscriptionRepository{}
var _ rateUserEventRepository = &repository.RateUserEventRepository{}

func newTestService(t *testing.T,
	eh executionHistoryRepository,
	rs rateSourceRepository,
	rv rateValueRepository,
	rus rateUserSubscriptionRepository,
	rue rateUserEventRepository,
) *RateService {
	t.Helper()
	svc, err := NewWebRestAPI(eh, rs, rv, rus, rue)
	require.NoError(t, err)
	return svc
}

func TestRateService_ObtainLastNExecutionHistoryBySourceName(t *testing.T) {
	t.Parallel()

	t.Run("delegates and returns results", func(t *testing.T) {
		t.Parallel()

		want := []domain.ExecutionHistory{{ID: "h1", SourceName: "src1", Success: true}}
		repo := &mockExecutionHistoryRepository{items: want}
		svc := newTestService(t, repo, &mockRateSourceRepository{}, &mockRateValueRepository{}, &mockRateUserSubscriptionRepository{}, &mockRateUserEventRepository{})

		got, err := svc.ObtainLastNExecutionHistoryBySourceName(t.Context(), "src1", 5)
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.False(t, repo.capturedBool)
	})

	t.Run("error propagated", func(t *testing.T) {
		t.Parallel()

		repo := &mockExecutionHistoryRepository{err: errors.New("db error")}
		svc := newTestService(t, repo, &mockRateSourceRepository{}, &mockRateValueRepository{}, &mockRateUserSubscriptionRepository{}, &mockRateUserEventRepository{})

		_, err := svc.ObtainLastNExecutionHistoryBySourceName(t.Context(), "src1", 5)
		require.Error(t, err)
	})
}

func TestRateService_ObtainLastSuccessNExecutionHistoryBySourceName(t *testing.T) {
	t.Parallel()

	t.Run("calls repo with successOnly=true", func(t *testing.T) {
		t.Parallel()

		repo := &mockExecutionHistoryRepository{items: []domain.ExecutionHistory{{ID: "h1"}}}
		svc := newTestService(t, repo, &mockRateSourceRepository{}, &mockRateValueRepository{}, &mockRateUserSubscriptionRepository{}, &mockRateUserEventRepository{})

		_, err := svc.ObtainLastSuccessNExecutionHistoryBySourceName(t.Context(), "src1", 5)
		require.NoError(t, err)
		require.True(t, repo.capturedBool)
	})

	t.Run("error propagated", func(t *testing.T) {
		t.Parallel()

		repo := &mockExecutionHistoryRepository{err: errors.New("fail")}
		svc := newTestService(t, repo, &mockRateSourceRepository{}, &mockRateValueRepository{}, &mockRateUserSubscriptionRepository{}, &mockRateUserEventRepository{})

		_, err := svc.ObtainLastSuccessNExecutionHistoryBySourceName(t.Context(), "src1", 5)
		require.Error(t, err)
	})
}

func TestRateService_ObtainAllRateSources(t *testing.T) {
	t.Parallel()

	t.Run("returns sources", func(t *testing.T) {
		t.Parallel()

		want := []domain.RateSource{{Name: "src1"}, {Name: "src2"}}
		repo := &mockRateSourceRepository{sources: want}
		svc := newTestService(t, &mockExecutionHistoryRepository{}, repo, &mockRateValueRepository{}, &mockRateUserSubscriptionRepository{}, &mockRateUserEventRepository{})

		got, err := svc.ObtainAllRateSources(t.Context())
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("error propagated", func(t *testing.T) {
		t.Parallel()

		repo := &mockRateSourceRepository{err: errors.New("fail")}
		svc := newTestService(t, &mockExecutionHistoryRepository{}, repo, &mockRateValueRepository{}, &mockRateUserSubscriptionRepository{}, &mockRateUserEventRepository{})

		_, err := svc.ObtainAllRateSources(t.Context())
		require.Error(t, err)
	})
}

func TestRateService_ObtainLastNRateValuesBySourceName(t *testing.T) {
	t.Parallel()

	t.Run("delegates with correct args", func(t *testing.T) {
		t.Parallel()

		want := []domain.RateValue{{ID: "v1", Price: 470.0}}
		repo := &mockRateValueRepository{values: want}
		svc := newTestService(t, &mockExecutionHistoryRepository{}, &mockRateSourceRepository{}, repo, &mockRateUserSubscriptionRepository{}, &mockRateUserEventRepository{})

		got, err := svc.ObtainLastNRateValuesBySourceName(t.Context(), "src1", 10)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("error propagated", func(t *testing.T) {
		t.Parallel()

		repo := &mockRateValueRepository{err: errors.New("fail")}
		svc := newTestService(t, &mockExecutionHistoryRepository{}, &mockRateSourceRepository{}, repo, &mockRateUserSubscriptionRepository{}, &mockRateUserEventRepository{})

		_, err := svc.ObtainLastNRateValuesBySourceName(t.Context(), "src1", 10)
		require.Error(t, err)
	})
}

func TestRateService_ObtainListOfLastRateUserEvent(t *testing.T) {
	t.Parallel()

	t.Run("calls repo with all four statuses", func(t *testing.T) {
		t.Parallel()

		repo := &mockRateUserEventRepository{}
		svc := newTestService(t, &mockExecutionHistoryRepository{}, &mockRateSourceRepository{}, &mockRateValueRepository{}, &mockRateUserSubscriptionRepository{}, repo)

		_, err := svc.ObtainListOfLastRateUserEvent(t.Context(), 10)
		require.NoError(t, err)

		require.Contains(t, repo.statuses, domain.RateUserEventStatusPending)
		require.Contains(t, repo.statuses, domain.RateUserEventStatusSent)
		require.Contains(t, repo.statuses, domain.RateUserEventStatusFailed)
		require.Contains(t, repo.statuses, domain.RateUserEventStatusCanceled)
	})

	t.Run("error propagated", func(t *testing.T) {
		t.Parallel()

		repo := &mockRateUserEventRepository{err: errors.New("fail")}
		svc := newTestService(t, &mockExecutionHistoryRepository{}, &mockRateSourceRepository{}, &mockRateValueRepository{}, &mockRateUserSubscriptionRepository{}, repo)

		_, err := svc.ObtainListOfLastRateUserEvent(t.Context(), 10)
		require.Error(t, err)
	})
}

func TestRateService_ObtainFailedListOfRateUserEvent(t *testing.T) {
	t.Parallel()

	t.Run("delegates with offset and limit", func(t *testing.T) {
		t.Parallel()

		want := []domain.RateUserEvent{{ID: "e1"}}
		repo := &mockRateUserEventRepository{items: want}
		svc := newTestService(t, &mockExecutionHistoryRepository{}, &mockRateSourceRepository{}, &mockRateValueRepository{}, &mockRateUserSubscriptionRepository{}, repo)

		got, err := svc.ObtainFailedListOfRateUserEvent(t.Context(), 0, 10)
		require.NoError(t, err)
		require.Equal(t, want, got)
		// ObtainFailedListOfRateUserEvent passes no status args (empty variadic).
		require.Empty(t, repo.statuses)
	})

	t.Run("error propagated", func(t *testing.T) {
		t.Parallel()

		repo := &mockRateUserEventRepository{err: errors.New("fail")}
		svc := newTestService(t, &mockExecutionHistoryRepository{}, &mockRateSourceRepository{}, &mockRateValueRepository{}, &mockRateUserSubscriptionRepository{}, repo)

		_, err := svc.ObtainFailedListOfRateUserEvent(t.Context(), 0, 10)
		require.Error(t, err)
	})
}

type mockExecutionHistoryRepository struct {
	items        []domain.ExecutionHistory
	err          error
	capturedBool bool
}

func (m *mockExecutionHistoryRepository) ObtainLastNExecutionHistoryBySourceName(_ context.Context, _ string, _ int64, s bool) ([]domain.ExecutionHistory, error) {
	m.capturedBool = s
	return m.items, m.err
}

type mockRateSourceRepository struct {
	sources []domain.RateSource
	err     error
}

func (m *mockRateSourceRepository) ObtainAllRateSources(_ context.Context) ([]domain.RateSource, error) {
	return m.sources, m.err
}

type mockRateValueRepository struct {
	values []domain.RateValue
	err    error
}

func (m *mockRateValueRepository) ObtainLastNRateValuesBySourceName(_ context.Context, _ string, _ int64) ([]domain.RateValue, error) {
	return m.values, m.err
}

type mockRateUserSubscriptionRepository struct{}

func (m *mockRateUserSubscriptionRepository) ObtainRateUserSubscriptionsBySource(_ context.Context, _ string) ([]domain.RateUserSubscription, error) {
	return nil, nil
}

type mockRateUserEventRepository struct {
	items    []domain.RateUserEvent
	err      error
	statuses []domain.RateUserEventStatus
}

func (m *mockRateUserEventRepository) ObtainLastNRateUserEvents(_ context.Context, _, _ int64, s ...domain.RateUserEventStatus) ([]domain.RateUserEvent, error) {
	m.statuses = s
	return m.items, m.err
}
