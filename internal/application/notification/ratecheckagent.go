package notification

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
)

// RateCheckAgent reads all sources and subscriptions from the DB, evaluates which
// notifications are due, builds alert messages, and queues them as RateUserEvents.
// It is designed to run to completion on each invocation (one-shot), decoupled from
// the data-collection cycle.
type RateCheckAgent struct {
	rateSourceRepository           rateSourceRepository
	rateValueRepository            rateValueRepository
	rateUserSubscriptionRepository rateUserSubscriptionRepository
	rateUserEventRepository        rateCheckEventRepository
	logger                         io.Writer
}

// NewRateCheckAgent constructs a RateCheckAgent. All repository arguments are required.
func NewRateCheckAgent(
	rRateSource rateSourceRepository,
	rRateValue rateValueRepository,
	rRateUserSubscription rateUserSubscriptionRepository,
	rRateUserEvent rateCheckEventRepository,
	logger io.Writer,
) (*RateCheckAgent, error) {
	if rRateSource == nil || rRateValue == nil || rRateUserSubscription == nil || rRateUserEvent == nil {
		return nil, errors.New("all repository arguments are required")
	}
	return &RateCheckAgent{
		rateSourceRepository:           rRateSource,
		rateValueRepository:            rRateValue,
		rateUserSubscriptionRepository: rRateUserSubscription,
		rateUserEventRepository:        rRateUserEvent,
		logger:                         logger,
	}, nil
}

// Run iterates every rate source, checks active subscriptions, and queues alert events
// for any subscription whose notification condition is currently satisfied.
func (a *RateCheckAgent) Run(ctx context.Context) error {
	sources, err := a.rateSourceRepository.ObtainAllRateSources(ctx)
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}
	if len(sources) == 0 {
		return nil
	}

	now := time.Now().UTC()
	telegramBotAlerts := make(map[string][]alert, len(sources))
	errs := make(map[string]error, len(sources))

	for _, source := range sources {
		values, err := a.rateValueRepository.ObtainLastNRateValuesBySourceName(ctx, source.Name, 1)
		if err != nil {
			errs[source.Name] = errors.Join(errs[source.Name], errors.Join(err, internal.NewTraceError()))
			continue
		}
		if len(values) == 0 {
			continue
		}
		currentValue := values[0].Price

		subscriptions, err := a.rateUserSubscriptionRepository.ObtainRateUserSubscriptionsBySource(ctx, source.Name)
		if err != nil {
			errs[source.Name] = errors.Join(errs[source.Name], errors.Join(err, internal.NewTraceError()))
			continue
		}

		for _, subscription := range subscriptions {
			delta := currentValue - subscription.LatestNotifiedRate

			ok, err := subscription.IsDue(now, delta)
			if err != nil {
				errs[source.Name] = errors.Join(errs[source.Name], errors.Join(err, internal.NewTraceError()))
				continue
			}
			if !ok {
				continue
			}

			switch subscription.UserType {
			case domain.UserTypeTelegram:
				telegramBotAlerts[subscription.UserID] = append(telegramBotAlerts[subscription.UserID], alert{
					SourceName:    source.Name,
					SourceTitle:   source.Title,
					BaseCurrency:  source.BaseCurrency,
					QuoteCurrency: source.QuoteCurrency,
					CurrentPrice:  currentValue,
					Delta:         delta,
					Timestamp:     now,
				})
			default:
				errs[source.Name] = errors.Join(errs[source.Name], fmt.Errorf("unsupported user type: %s", subscription.UserType))
			}

			subscription.LatestNotifiedRate = currentValue
			if err = a.rateUserSubscriptionRepository.RetainRateUserSubscription(ctx, &subscription); err != nil {
				errs[source.Name] = errors.Join(errs[source.Name], errors.Join(err, internal.NewTraceError()))
				continue
			}
		}
	}

	for chatID, alerts := range telegramBotAlerts {
		msgs, err := buildAlertMessage(alerts...)
		if err != nil {
			log.Printf("notification check: build message for chat_id=%s failed: %s", chatID, err)
			continue
		}
		var failCount int
		for _, msg := range msgs {
			if retainErr := a.rateUserEventRepository.RetainRateUserEvent(ctx, &domain.RateUserEvent{
				UserType: domain.UserTypeTelegram,
				UserID:   chatID,
				Message:  msg,
			}); retainErr != nil {
				failCount++
			}
		}
		log.Printf("notification check: chat_id=%s queued: %d/%d", chatID, len(msgs)-failCount, len(msgs))
	}

	combined := make([]error, 0, len(errs))
	for k, e := range errs {
		if e != nil {
			combined = append(combined, fmt.Errorf("source %s: %w", k, e))
		}
	}
	return errors.Join(combined...)
}

type rateSourceRepository interface {
	ObtainAllRateSources(context.Context) ([]domain.RateSource, error)
}

type rateValueRepository interface {
	ObtainLastNRateValuesBySourceName(context.Context, string, int64) ([]domain.RateValue, error)
}

type rateUserSubscriptionRepository interface {
	ObtainRateUserSubscriptionsBySource(context.Context, string) ([]domain.RateUserSubscription, error)
	RetainRateUserSubscription(context.Context, *domain.RateUserSubscription) error
}

// rateCheckEventRepository is intentionally narrower than rateUserEventRepository
// used by RateDispatchAgent — each type declares only what it needs.
type rateCheckEventRepository interface {
	RetainRateUserEvent(context.Context, *domain.RateUserEvent) error
}
