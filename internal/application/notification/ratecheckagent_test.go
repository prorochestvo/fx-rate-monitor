package notification

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/repository"
	"github.com/stretchr/testify/require"
)

var _ rateSourceRepository = &repository.RateSourceRepository{}
var _ rateValueRepository = &repository.RateValueRepository{}
var _ rateUserSubscriptionRepository = &repository.RateUserSubscriptionRepository{}
var _ rateCheckEventRepository = &repository.RateUserEventRepository{}

func TestNewRateCheckAgent(t *testing.T) {
	t.Parallel()

	t.Run("valid construction", func(t *testing.T) {
		t.Parallel()

		agent, err := NewRateCheckAgent(
			&mockCheckSourceRepository{},
			&mockCheckValueRepository{},
			&mockCheckSubscriptionRepository{},
			&mockCheckEventRepository{},
			io.Discard,
		)
		require.NoError(t, err)
		require.NotNil(t, agent)
	})
	t.Run("nil rateSourceRepository returns error", func(t *testing.T) {
		t.Parallel()

		_, err := NewRateCheckAgent(nil, &mockCheckValueRepository{}, &mockCheckSubscriptionRepository{}, &mockCheckEventRepository{}, io.Discard)
		require.Error(t, err)
	})
	t.Run("nil rateValueRepository returns error", func(t *testing.T) {
		t.Parallel()

		_, err := NewRateCheckAgent(&mockCheckSourceRepository{}, nil, &mockCheckSubscriptionRepository{}, &mockCheckEventRepository{}, io.Discard)
		require.Error(t, err)
	})
	t.Run("nil rateUserSubscriptionRepository returns error", func(t *testing.T) {
		t.Parallel()

		_, err := NewRateCheckAgent(&mockCheckSourceRepository{}, &mockCheckValueRepository{}, nil, &mockCheckEventRepository{}, io.Discard)
		require.Error(t, err)
	})
	t.Run("nil rateUserEventRepository returns error", func(t *testing.T) {
		t.Parallel()

		_, err := NewRateCheckAgent(&mockCheckSourceRepository{}, &mockCheckValueRepository{}, &mockCheckSubscriptionRepository{}, nil, io.Discard)
		require.Error(t, err)
	})
}

func TestRateCheckAgent_Run(t *testing.T) {
	t.Parallel()

	t.Run("no sources — returns nil, no events queued", func(t *testing.T) {
		t.Parallel()

		eventRepo := &mockCheckEventRepository{}
		a := &RateCheckAgent{
			rateSourceRepository:           &mockCheckSourceRepository{sources: nil},
			rateValueRepository:            &mockCheckValueRepository{},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{},
			rateUserEventRepository:        eventRepo,
		}

		require.NoError(t, a.Run(t.Context()))
		require.Empty(t, eventRepo.retained)
	})
	t.Run("sources with no rate values — returns nil, no events queued", func(t *testing.T) {
		t.Parallel()

		eventRepo := &mockCheckEventRepository{}
		a := &RateCheckAgent{
			rateSourceRepository:           &mockCheckSourceRepository{sources: []domain.RateSource{{Name: "src1"}}},
			rateValueRepository:            &mockCheckValueRepository{values: nil},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{},
			rateUserEventRepository:        eventRepo,
		}

		require.NoError(t, a.Run(t.Context()))
		require.Empty(t, eventRepo.retained)
	})
	t.Run("sources with rate values but no subscriptions — returns nil", func(t *testing.T) {
		t.Parallel()

		eventRepo := &mockCheckEventRepository{}
		a := &RateCheckAgent{
			rateSourceRepository:           &mockCheckSourceRepository{sources: []domain.RateSource{{Name: "src1"}}},
			rateValueRepository:            &mockCheckValueRepository{values: []domain.RateValue{{Price: 100}}},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{subs: nil},
			rateUserEventRepository:        eventRepo,
		}

		require.NoError(t, a.Run(t.Context()))
		require.Empty(t, eventRepo.retained)
	})
	t.Run("subscription condition satisfied — event retained", func(t *testing.T) {
		t.Parallel()

		eventRepo := &mockCheckEventRepository{}
		a := &RateCheckAgent{
			rateSourceRepository: &mockCheckSourceRepository{
				sources: []domain.RateSource{{Name: "src1", Title: "Test Bank", BaseCurrency: "USD", QuoteCurrency: "KZT"}},
			},
			rateValueRepository: &mockCheckValueRepository{values: []domain.RateValue{{Price: 100}}},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{
				subs: []domain.RateUserSubscription{{
					UserType:       domain.UserTypeTelegram,
					UserID:         "111",
					ConditionType:  domain.ConditionTypeDelta,
					ConditionValue: "0",
				}},
			},
			rateUserEventRepository: eventRepo,
		}

		require.NoError(t, a.Run(t.Context()))
		require.Len(t, eventRepo.retained, 1)
		require.Equal(t, domain.UserTypeTelegram, eventRepo.retained[0].UserType)
		require.Equal(t, "111", eventRepo.retained[0].UserID)
	})
	t.Run("subscription condition not satisfied — no event retained", func(t *testing.T) {
		t.Parallel()

		eventRepo := &mockCheckEventRepository{}
		a := &RateCheckAgent{
			rateSourceRepository: &mockCheckSourceRepository{sources: []domain.RateSource{{Name: "src1"}}},
			rateValueRepository:  &mockCheckValueRepository{values: []domain.RateValue{{Price: 1}}},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{
				// delta threshold of 100; current price delta is 0 (LatestNotifiedRate=0 → forced to 0)
				// but delta=0 satisfies any threshold per IsDeltaSatisfied, so use interval type
				subs: []domain.RateUserSubscription{{
					UserType:           domain.UserTypeTelegram,
					UserID:             "222",
					ConditionType:      domain.ConditionTypeInterval,
					ConditionValue:     "1h",
					LatestNotifiedRate: 1,
				}},
			},
			rateUserEventRepository: eventRepo,
		}

		// UpdatedAt is zero → IsIntervalDue returns true immediately for zero UpdatedAt.
		// Use a recent UpdatedAt to make it not due.
		// We need to set UpdatedAt via the mock.
		// Actually with ConditionTypeInterval and UpdatedAt=zero, it IS due.
		// Let's use a fresh sub that was just updated:
		// We test not-due by providing a subscription where IsDue returns false.
		// The easiest is ConditionTypeInterval with a very long interval and UpdatedAt=now.
		// But we can't set time in the mock easily.
		// Instead: use ConditionTypeDelta with threshold > 0 and LatestNotifiedRate=currentValue (delta=0).
		// With delta=0: IsDeltaSatisfied checks if d==0 (it is) → returns true. That satisfies.
		// So let's use delta threshold with LatestNotifiedRate=current-price-minus-small-amount:
		// current=1, LatestNotifiedRate=0.99 → delta=0.01 < threshold=1.0 → not satisfied.
		_ = a // suppress unused warning — we reconfigure below
		eventRepo2 := &mockCheckEventRepository{}
		a2 := &RateCheckAgent{
			rateSourceRepository: &mockCheckSourceRepository{sources: []domain.RateSource{{Name: "src1"}}},
			rateValueRepository:  &mockCheckValueRepository{values: []domain.RateValue{{Price: 1.0}}},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{
				subs: []domain.RateUserSubscription{{
					UserType:           domain.UserTypeTelegram,
					UserID:             "222",
					ConditionType:      domain.ConditionTypeDelta,
					ConditionValue:     "1.0",
					LatestNotifiedRate: 0.99, // delta = 0.01 < 1.0 threshold → not due
				}},
			},
			rateUserEventRepository: eventRepo2,
		}

		require.NoError(t, a2.Run(t.Context()))
		require.Empty(t, eventRepo2.retained)
	})
	t.Run("source repo error — error returned", func(t *testing.T) {
		t.Parallel()

		a := &RateCheckAgent{
			rateSourceRepository:           &mockCheckSourceRepository{err: errors.New("source db fail")},
			rateValueRepository:            &mockCheckValueRepository{},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{},
			rateUserEventRepository:        &mockCheckEventRepository{},
		}

		require.Error(t, a.Run(t.Context()))
	})
	t.Run("rate value repo error — error returned for that source", func(t *testing.T) {
		t.Parallel()

		a := &RateCheckAgent{
			rateSourceRepository:           &mockCheckSourceRepository{sources: []domain.RateSource{{Name: "src1"}}},
			rateValueRepository:            &mockCheckValueRepository{err: errors.New("value db fail")},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{},
			rateUserEventRepository:        &mockCheckEventRepository{},
		}

		err := a.Run(t.Context())
		require.Error(t, err)
		require.Contains(t, err.Error(), "src1")
	})
	t.Run("subscription repo error — error returned for that source", func(t *testing.T) {
		t.Parallel()

		a := &RateCheckAgent{
			rateSourceRepository:           &mockCheckSourceRepository{sources: []domain.RateSource{{Name: "src1"}}},
			rateValueRepository:            &mockCheckValueRepository{values: []domain.RateValue{{Price: 100}}},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{err: errors.New("sub db fail")},
			rateUserEventRepository:        &mockCheckEventRepository{},
		}

		err := a.Run(t.Context())
		require.Error(t, err)
		require.Contains(t, err.Error(), "src1")
	})
	t.Run("unsupported UserType — error entry in result", func(t *testing.T) {
		t.Parallel()

		a := &RateCheckAgent{
			rateSourceRepository: &mockCheckSourceRepository{sources: []domain.RateSource{{Name: "src1"}}},
			rateValueRepository:  &mockCheckValueRepository{values: []domain.RateValue{{Price: 100}}},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{
				subs: []domain.RateUserSubscription{{
					UserType:       "bogus",
					UserID:         "999",
					ConditionType:  domain.ConditionTypeDelta,
					ConditionValue: "0",
				}},
			},
			rateUserEventRepository: &mockCheckEventRepository{},
		}

		err := a.Run(t.Context())
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported user type")
	})
	t.Run("subscription retain error appears in error map", func(t *testing.T) {
		t.Parallel()

		a := &RateCheckAgent{
			rateSourceRepository: &mockCheckSourceRepository{sources: []domain.RateSource{{Name: "src1"}}},
			rateValueRepository:  &mockCheckValueRepository{values: []domain.RateValue{{Price: 100}}},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{
				subs: []domain.RateUserSubscription{{
					UserType:       domain.UserTypeTelegram,
					UserID:         "555",
					ConditionType:  domain.ConditionTypeDelta,
					ConditionValue: "0",
				}},
				retainErr: errors.New("retain fail"),
			},
			rateUserEventRepository: &mockCheckEventRepository{},
		}

		err := a.Run(t.Context())
		require.Error(t, err)
		require.Contains(t, err.Error(), "src1")
	})
	t.Run("delta type unchanged rate does not fire after first notification", func(t *testing.T) {
		t.Parallel()
		// Regression: notifications sent every 10 minutes when rate was stable.
		// LatestNotifiedRate == currentValue → delta == 0 with LatestNotifiedRate > 0 → must not fire.
		eventRepo := &mockCheckEventRepository{}
		a := &RateCheckAgent{
			rateSourceRepository: &mockCheckSourceRepository{
				sources: []domain.RateSource{{
					Name: "KZ_QAZPOST_BID_USD_KZT", Title: "QazPost",
					BaseCurrency: "USD", QuoteCurrency: "KZT",
				}},
			},
			rateValueRepository: &mockCheckValueRepository{
				values: []domain.RateValue{{Price: 470.0}},
			},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{
				subs: []domain.RateUserSubscription{{
					UserType:           domain.UserTypeTelegram,
					UserID:             "115818690",
					ConditionType:      domain.ConditionTypeDelta,
					ConditionValue:     "1.0",
					LatestNotifiedRate: 470.0, // already notified at this price
				}},
			},
			rateUserEventRepository: eventRepo,
		}

		require.NoError(t, a.Run(t.Context()))
		require.Empty(t, eventRepo.retained, "no notification expected when rate is unchanged")
	})
	t.Run("two sources same user — consolidated into one event", func(t *testing.T) {
		t.Parallel()

		eventRepo := &mockCheckEventRepository{}
		a := &RateCheckAgent{
			rateSourceRepository: &mockCheckSourceRepository{
				sources: []domain.RateSource{
					{Name: "SRC_A", Title: "Alpha Bank", BaseCurrency: "USD", QuoteCurrency: "KZT"},
					{Name: "SRC_B", Title: "Beta Bank", BaseCurrency: "EUR", QuoteCurrency: "KZT"},
				},
			},
			rateValueRepository: &mockCheckValueRepository{values: []domain.RateValue{{Price: 475}}},
			rateUserSubscriptionRepository: &mockCheckSubscriptionRepository{
				subs: []domain.RateUserSubscription{{
					UserType:       domain.UserTypeTelegram,
					UserID:         "115818690",
					ConditionType:  domain.ConditionTypeDelta,
					ConditionValue: "0",
				}},
			},
			rateUserEventRepository: eventRepo,
		}

		require.NoError(t, a.Run(t.Context()))
		require.Len(t, eventRepo.retained, 1)
		require.Contains(t, eventRepo.retained[0].Message, "Alpha Bank")
		require.Contains(t, eventRepo.retained[0].Message, "Beta Bank")
	})
}

type mockCheckSourceRepository struct {
	sources []domain.RateSource
	err     error
}

func (m *mockCheckSourceRepository) ObtainAllRateSources(_ context.Context) ([]domain.RateSource, error) {
	return m.sources, m.err
}

type mockCheckValueRepository struct {
	values []domain.RateValue
	err    error
}

func (m *mockCheckValueRepository) ObtainLastNRateValuesBySourceName(_ context.Context, _ string, _ int64) ([]domain.RateValue, error) {
	return m.values, m.err
}

type mockCheckSubscriptionRepository struct {
	subs      []domain.RateUserSubscription
	err       error
	retainErr error
}

func (m *mockCheckSubscriptionRepository) ObtainRateUserSubscriptionsBySource(_ context.Context, _ string) ([]domain.RateUserSubscription, error) {
	return m.subs, m.err
}

func (m *mockCheckSubscriptionRepository) RetainRateUserSubscription(_ context.Context, _ *domain.RateUserSubscription) error {
	if m.retainErr != nil {
		return m.retainErr
	}
	return m.err
}

type mockCheckEventRepository struct {
	retained []*domain.RateUserEvent
	err      error
}

func (m *mockCheckEventRepository) RetainRateUserEvent(_ context.Context, e *domain.RateUserEvent) error {
	cp := *e
	m.retained = append(m.retained, &cp)
	return m.err
}
