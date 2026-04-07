package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	integration "github.com/seilbekskindirov/monitor/internal/infrastructure/telegrambot"
	"github.com/seilbekskindirov/monitor/internal/repository"
	"github.com/stretchr/testify/require"
)

var _ rateUserEventRepository = &repository.RateUserEventRepository{}

func TestNewRateAgent(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		agent, err := NewRateAgent(&mockTelegramClient{}, &mockRateUserEventRepository{})
		require.NoError(t, err)
		require.NotNil(t, agent)
	})
}

func TestRateAgent_Run(t *testing.T) {
	t.Parallel()

	t.Run("pending telegram event is sent and marked sent", func(t *testing.T) {
		t.Parallel()

		repo := &mockRateUserEventRepository{
			events: []domain.RateUserEvent{
				{
					UserType:  domain.UserTypeTelegram,
					UserID:    "123456",
					Message:   "hello",
					Status:    domain.RateUserEventStatusPending,
					CreatedAt: time.Now().UTC(),
				},
			},
		}
		tg := &mockTelegramClient{}

		agent, err := NewRateAgent(tg, repo)
		require.NoError(t, err)

		require.NoError(t, agent.Run(t.Context()))
		require.Len(t, repo.retained, 1)
		require.Equal(t, domain.RateUserEventStatusSent, repo.retained[0].Status)
		require.False(t, repo.retained[0].SentAt.IsZero())
	})

	t.Run("telegram failure marks event failed", func(t *testing.T) {
		t.Parallel()

		repo := &mockRateUserEventRepository{
			events: []domain.RateUserEvent{
				{
					UserType:  domain.UserTypeTelegram,
					UserID:    "123456",
					Message:   "hello",
					Status:    domain.RateUserEventStatusPending,
					CreatedAt: time.Now().UTC(),
				},
			},
		}
		tg := &mockTelegramClient{err: errors.New("send failed")}

		agent, err := NewRateAgent(tg, repo)
		require.NoError(t, err)

		_ = agent.Run(t.Context())
		require.Len(t, repo.retained, 1)
		require.Equal(t, domain.RateUserEventStatusFailed, repo.retained[0].Status)
		require.NotEmpty(t, repo.retained[0].LastError)
	})

	t.Run("event older than 24h TTL is canceled", func(t *testing.T) {
		t.Parallel()

		repo := &mockRateUserEventRepository{
			events: []domain.RateUserEvent{
				{
					UserType:  domain.UserTypeTelegram,
					UserID:    "123456",
					Message:   "stale",
					Status:    domain.RateUserEventStatusPending,
					CreatedAt: time.Now().UTC().Add(-25 * time.Hour),
				},
			},
		}
		tg := &mockTelegramClient{}

		agent, err := NewRateAgent(tg, repo)
		require.NoError(t, err)

		require.NoError(t, agent.Run(t.Context()))
		require.Len(t, repo.retained, 1)
		require.Equal(t, domain.RateUserEventStatusCanceled, repo.retained[0].Status)
		require.Equal(t, 0, tg.calls)
	})

	t.Run("unsupported user type returns error", func(t *testing.T) {
		t.Parallel()

		repo := &mockRateUserEventRepository{
			events: []domain.RateUserEvent{
				{
					UserType:  "bogus",
					UserID:    "123456",
					Message:   "msg",
					Status:    domain.RateUserEventStatusPending,
					CreatedAt: time.Now().UTC(),
				},
			},
		}

		agent, err := NewRateAgent(&mockTelegramClient{}, repo)
		require.NoError(t, err)

		err = agent.Run(t.Context())
		require.Error(t, err)
	})

	t.Run("empty event list returns nil error", func(t *testing.T) {
		t.Parallel()

		repo := &mockRateUserEventRepository{events: []domain.RateUserEvent{}}
		agent, err := NewRateAgent(&mockTelegramClient{}, repo)
		require.NoError(t, err)

		require.NoError(t, agent.Run(t.Context()))
	})
}

func TestRateAgent_runUserTypeTelegram(t *testing.T) {
	t.Parallel()

	t.Run("valid chat ID sends message", func(t *testing.T) {
		t.Parallel()

		tg := &mockTelegramClient{}
		agent, err := NewRateAgent(tg, &mockRateUserEventRepository{})
		require.NoError(t, err)

		event := &domain.RateUserEvent{UserID: "123456", Message: "hi"}
		require.NoError(t, agent.runUserTypeTelegram(t.Context(), event))
		require.Equal(t, 1, tg.calls)
	})

	t.Run("non-numeric UserID returns error", func(t *testing.T) {
		t.Parallel()

		agent, err := NewRateAgent(&mockTelegramClient{}, &mockRateUserEventRepository{})
		require.NoError(t, err)

		event := &domain.RateUserEvent{UserID: "abc", Message: "hi"}
		require.Error(t, agent.runUserTypeTelegram(t.Context(), event))
	})

	t.Run("zero UserID returns error", func(t *testing.T) {
		t.Parallel()

		agent, err := NewRateAgent(&mockTelegramClient{}, &mockRateUserEventRepository{})
		require.NoError(t, err)

		event := &domain.RateUserEvent{UserID: "0", Message: "hi"}
		require.Error(t, agent.runUserTypeTelegram(t.Context(), event))
	})

	t.Run("telegram client error propagated", func(t *testing.T) {
		t.Parallel()

		tg := &mockTelegramClient{err: errors.New("network error")}
		agent, err := NewRateAgent(tg, &mockRateUserEventRepository{})
		require.NoError(t, err)

		event := &domain.RateUserEvent{UserID: "123456", Message: "hi"}
		require.Error(t, agent.runUserTypeTelegram(t.Context(), event))
	})

	t.Run("nil event returns error", func(t *testing.T) {
		t.Parallel()

		agent, err := NewRateAgent(&mockTelegramClient{}, &mockRateUserEventRepository{})
		require.NoError(t, err)

		require.Error(t, agent.runUserTypeTelegram(t.Context(), nil))
	})
}

func TestRateAgent_Vacuum(t *testing.T) {
	t.Parallel()

	t.Run("calls repo with 180-day duration", func(t *testing.T) {
		t.Parallel()

		repo := &mockRateUserEventRepository{}
		agent, err := NewRateAgent(&mockTelegramClient{}, repo)
		require.NoError(t, err)

		require.NoError(t, agent.Vacuum(t.Context()))
		require.Equal(t, 180*24*time.Hour, repo.removedDuration)
	})

	t.Run("repo error is propagated", func(t *testing.T) {
		t.Parallel()

		repo := &mockRateUserEventRepository{retainErr: errors.New("db error")}
		repo.removeErr = errors.New("remove failed")
		agent, err := NewRateAgent(&mockTelegramClient{}, repo)
		require.NoError(t, err)

		require.Error(t, agent.Vacuum(t.Context()))
	})
}

type mockRateUserEventRepository struct {
	events          []domain.RateUserEvent
	retained        []*domain.RateUserEvent
	retainErr       error
	removeErr       error
	removedDuration time.Duration
}

func (m *mockRateUserEventRepository) ObtainUnprocessedRateUserEvents(_ context.Context) ([]domain.RateUserEvent, error) {
	return m.events, nil
}

func (m *mockRateUserEventRepository) RetainRateUserEvent(_ context.Context, e *domain.RateUserEvent) error {
	cp := *e
	m.retained = append(m.retained, &cp)
	return m.retainErr
}

func (m *mockRateUserEventRepository) RemoveRateUserEventOlderThan(_ context.Context, d time.Duration) error {
	m.removedDuration = d
	return m.removeErr
}

type mockTelegramClient struct {
	err   error
	calls int
}

func (m *mockTelegramClient) SendHTMLMessage(_ context.Context, _ integration.TelegramChatID, _ string) error {
	m.calls++
	return m.err
}
