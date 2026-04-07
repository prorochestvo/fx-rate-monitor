package collection

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/repository"
	"github.com/stretchr/testify/require"
)

var _ executionHistoryRepository = &repository.ExecutionHistoryRepository{}
var _ rateSourceRepository = &repository.RateSourceRepository{}
var _ rateValueRepository = &repository.RateValueRepository{}
var _ rateUserSubscriptionRepository = &repository.RateUserSubscriptionRepository{}
var _ rateUserEventRepository = &repository.RateUserEventRepository{}

func TestNewRateAgent(t *testing.T) {
	t.Parallel()

	t.Run("valid construction", func(t *testing.T) {
		t.Parallel()

		agent, err := NewRateAgent(
			"",
			&mockRateSourceRepository{},
			&mockExecutionHistoryRepository{},
			&mockRateValueRepository{},
			&mockRateUserSubscriptionRepository{},
			&mockRateUserEventRepository{},
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

func TestRateAgent_notification(t *testing.T) {
	t.Parallel()

	t.Run("two values — telegram event retained", func(t *testing.T) {
		t.Parallel()

		eventRepo := &mockRateUserEventRepository{}
		a := &RateAgent{
			rateValueRepository: &mockRateValueRepository{
				values: []domain.RateValue{{Price: 100}, {Price: 99}},
			},
			rateUserSubscriptionRepository: &mockRateUserSubscriptionRepository{
				subs: []domain.RateUserSubscription{{
					UserType: domain.UserTypeTelegram,
					UserID:   "111",
				}},
			},
			rateUserEventRepository: eventRepo,
			logger:                  io.Discard,
		}

		errs := a.notification(t.Context(), []domain.RateSource{{Name: "src1"}})
		require.Empty(t, errs)
		require.Len(t, eventRepo.retained, 1)
	})

	t.Run("one value — event retained", func(t *testing.T) {
		t.Parallel()

		eventRepo := &mockRateUserEventRepository{}
		a := &RateAgent{
			rateValueRepository: &mockRateValueRepository{
				values: []domain.RateValue{{Price: 100}},
			},
			rateUserSubscriptionRepository: &mockRateUserSubscriptionRepository{
				subs: []domain.RateUserSubscription{{
					UserType: domain.UserTypeTelegram,
					UserID:   "222",
				}},
			},
			rateUserEventRepository: eventRepo,
			logger:                  io.Discard,
		}

		errs := a.notification(t.Context(), []domain.RateSource{{Name: "src1"}})
		require.Empty(t, errs)
		require.Len(t, eventRepo.retained, 1)
	})

	t.Run("no values — no event retained", func(t *testing.T) {
		t.Parallel()

		eventRepo := &mockRateUserEventRepository{}
		a := &RateAgent{
			rateValueRepository: &mockRateValueRepository{values: []domain.RateValue{}},
			rateUserSubscriptionRepository: &mockRateUserSubscriptionRepository{
				subs: []domain.RateUserSubscription{{
					UserType: domain.UserTypeTelegram,
					UserID:   "333",
				}},
			},
			rateUserEventRepository: eventRepo,
			logger:                  io.Discard,
		}

		errs := a.notification(t.Context(), []domain.RateSource{{Name: "src1"}})
		require.Empty(t, errs)
		require.Len(t, eventRepo.retained, 0)
	})

	t.Run("no subscriptions — no event retained", func(t *testing.T) {
		t.Parallel()

		eventRepo := &mockRateUserEventRepository{}
		a := &RateAgent{
			rateValueRepository:            &mockRateValueRepository{values: []domain.RateValue{{Price: 100}}},
			rateUserSubscriptionRepository: &mockRateUserSubscriptionRepository{subs: nil},
			rateUserEventRepository:        eventRepo,
			logger:                         io.Discard,
		}

		errs := a.notification(t.Context(), []domain.RateSource{{Name: "src1"}})
		require.Empty(t, errs)
		require.Len(t, eventRepo.retained, 0)
	})

	t.Run("unsupported UserType — error map entry", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateValueRepository: &mockRateValueRepository{values: []domain.RateValue{{Price: 100}}},
			rateUserSubscriptionRepository: &mockRateUserSubscriptionRepository{
				subs: []domain.RateUserSubscription{{
					UserType: "bogus",
					UserID:   "444",
				}},
			},
			rateUserEventRepository: &mockRateUserEventRepository{},
			logger:                  io.Discard,
		}

		errs := a.notification(t.Context(), []domain.RateSource{{Name: "src1"}})
		require.NotNil(t, errs["src1"])
	})

	t.Run("rate value repo error — error map entry", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateValueRepository:            &mockRateValueRepository{err: errors.New("db fail")},
			rateUserSubscriptionRepository: &mockRateUserSubscriptionRepository{},
			rateUserEventRepository:        &mockRateUserEventRepository{},
			logger:                         io.Discard,
		}

		errs := a.notification(t.Context(), []domain.RateSource{{Name: "src1"}})
		require.NotNil(t, errs["src1"])
	})
}

func TestBuildAlertMessage(t *testing.T) {
	t.Parallel()

	t.Run("single alert produces one message", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:   "National Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.True(t, strings.Contains(msgs[0], "USD/KZT"))
	})

	t.Run("delta zero — no arrow in message", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:   "Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			Delta:         0,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.False(t, strings.Contains(msgs[0], telegramBotArrowUp))
		require.False(t, strings.Contains(msgs[0], telegramBotArrowDown))
	})

	t.Run("positive delta — up arrow shown", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:   "Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  100,
			Delta:         1.5,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.True(t, strings.Contains(msgs[0], telegramBotArrowUp))
	})

	t.Run("negative delta — down arrow shown", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:   "Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  100,
			Delta:         -1.5,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.True(t, strings.Contains(msgs[0], telegramBotArrowDown))
	})

	t.Run("forecast shown when ForecastMethod non-empty", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:    "Bank",
			BaseCurrency:   "USD",
			QuoteCurrency:  "KZT",
			CurrentPrice:   100,
			ForecastMethod: "composite",
			ForecastPrice:  99.9,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.True(t, strings.Contains(msgs[0], telegramBotForecast))
	})

	t.Run("forecast absent when ForecastMethod empty", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:    "Bank",
			BaseCurrency:   "USD",
			QuoteCurrency:  "KZT",
			CurrentPrice:   100,
			ForecastMethod: "",
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.False(t, strings.Contains(msgs[0], telegramBotForecast))
	})

	t.Run("no alerts — empty slice", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage()
		require.NoError(t, err)
		require.Len(t, msgs, 0)
	})

	t.Run("messages split above 2048 chars", func(t *testing.T) {
		t.Parallel()

		alerts := make([]alert, 50)
		for i := range alerts {
			alerts[i] = alert{
				SourceTitle:   strings.Repeat("X", 40) + string(rune('A'+i%26)),
				BaseCurrency:  "USD",
				QuoteCurrency: "KZT",
				CurrentPrice:  float64(400 + i),
			}
		}

		msgs, err := buildAlertMessage(alerts...)
		require.NoError(t, err)
		require.Greater(t, len(msgs), 1)
	})
}

func TestRateAgent_Run(t *testing.T) {
	t.Parallel()

	t.Run("no sources — returns nil", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateSourceRepository:           &mockRateSourceRepository{sources: nil},
			executionHistoryRepository:     &mockExecutionHistoryRepository{},
			rateValueRepository:            &mockRateValueRepository{},
			rateUserSubscriptionRepository: &mockRateUserSubscriptionRepository{},
			rateUserEventRepository:        &mockRateUserEventRepository{},
			rateExtractor:                  &mockRateExtractor{},
			logger:                         io.Discard,
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
			rateSourceRepository:           &mockRateSourceRepository{sources: []domain.RateSource{{Name: "src1", Interval: "1h"}}},
			executionHistoryRepository:     histRepo,
			rateValueRepository:            &mockRateValueRepository{},
			rateUserSubscriptionRepository: &mockRateUserSubscriptionRepository{},
			rateUserEventRepository:        &mockRateUserEventRepository{},
			rateExtractor:                  extractor,
			logger:                         io.Discard,
		}

		require.NoError(t, a.Run(t.Context()))
		require.Equal(t, 0, extractor.calls)
	})

	t.Run("source due — execution and notification run", func(t *testing.T) {
		t.Parallel()

		histRepo := &mockExecutionHistoryRepository{records: nil}
		eventRepo := &mockRateUserEventRepository{}
		a := &RateAgent{
			rateSourceRepository: &mockRateSourceRepository{
				sources: []domain.RateSource{{Name: "src1", Interval: "1m", Title: "SRC"}},
			},
			executionHistoryRepository: histRepo,
			rateValueRepository: &mockRateValueRepository{
				values: []domain.RateValue{{Price: 100}},
			},
			rateUserSubscriptionRepository: &mockRateUserSubscriptionRepository{
				subs: []domain.RateUserSubscription{{
					UserType: domain.UserTypeTelegram,
					UserID:   "999",
				}},
			},
			rateUserEventRepository: eventRepo,
			rateExtractor:           &mockRateExtractor{},
			logger:                  io.Discard,
		}

		require.NoError(t, a.Run(t.Context()))
		require.NotEmpty(t, histRepo.retained)
		require.NotEmpty(t, eventRepo.retained)
	})

	t.Run("invalid interval — error returned", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateSourceRepository: &mockRateSourceRepository{
				sources: []domain.RateSource{{Name: "src1", Interval: "bad"}},
			},
			executionHistoryRepository:     &mockExecutionHistoryRepository{},
			rateValueRepository:            &mockRateValueRepository{},
			rateUserSubscriptionRepository: &mockRateUserSubscriptionRepository{},
			rateUserEventRepository:        &mockRateUserEventRepository{},
			rateExtractor:                  &mockRateExtractor{},
			logger:                         io.Discard,
		}

		require.Error(t, a.Run(t.Context()))
	})

	t.Run("extractor error and value-repo error both surfaced", func(t *testing.T) {
		t.Parallel()

		a := &RateAgent{
			rateSourceRepository: &mockRateSourceRepository{
				sources: []domain.RateSource{{Name: "src1", Interval: "1m"}},
			},
			executionHistoryRepository: &mockExecutionHistoryRepository{},
			rateValueRepository:        &mockRateValueRepository{err: errors.New("db fail")},
			rateUserSubscriptionRepository: &mockRateUserSubscriptionRepository{
				subs: []domain.RateUserSubscription{{UserType: domain.UserTypeTelegram, UserID: "1"}},
			},
			rateUserEventRepository: &mockRateUserEventRepository{},
			rateExtractor:           &mockRateExtractor{err: errors.New("fetch error")},
			logger:                  io.Discard,
		}

		require.Error(t, a.Run(t.Context()))
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
	records  []domain.ExecutionHistory
	retained []*domain.ExecutionHistory
	err      error
}

func (m *mockExecutionHistoryRepository) RetainExecutionHistory(_ context.Context, h *domain.ExecutionHistory) error {
	cp := *h
	m.retained = append(m.retained, &cp)
	return m.err
}

func (m *mockExecutionHistoryRepository) ObtainLastNExecutionHistoryBySourceName(_ context.Context, _ string, _ int64, _ bool) ([]domain.ExecutionHistory, error) {
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

type mockRateUserSubscriptionRepository struct {
	subs []domain.RateUserSubscription
	err  error
}

func (m *mockRateUserSubscriptionRepository) ObtainRateUserSubscriptionsBySource(_ context.Context, _ string) ([]domain.RateUserSubscription, error) {
	return m.subs, m.err
}

type mockRateUserEventRepository struct {
	retained []*domain.RateUserEvent
	err      error
}

func (m *mockRateUserEventRepository) RetainRateUserEvent(_ context.Context, e *domain.RateUserEvent) error {
	cp := *e
	m.retained = append(m.retained, &cp)
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
