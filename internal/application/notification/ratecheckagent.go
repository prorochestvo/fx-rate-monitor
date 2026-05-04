package notification

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strconv"
	"strings"
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

// dedupBucket holds a single alert and the collapsed trigger values for each condition type
// that has fired on the same (source, base, quote, kind) key for one user.
type dedupBucket struct {
	a        alert
	triggers map[domain.SubscriptionConditionType]string
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

		// userBuckets groups fired subscriptions by (userID → dedupKey → bucket).
		// All subscriptions on the same dedupKey for the same user produce one alert bullet,
		// but every fired subscription still has its LatestNotifiedRate advanced via Retain.
		userBuckets := make(map[string]map[string]*dedupBucket)

		key := source.Name + "|" + source.BaseCurrency + "|" + source.QuoteCurrency + "|" + string(source.Kind)

		for _, sub := range subscriptions {
			delta := currentValue - sub.LatestNotifiedRate

			ok, err := sub.IsDue(now, delta)
			if err != nil {
				errs[source.Name] = errors.Join(errs[source.Name], errors.Join(err, internal.NewTraceError()))
				continue
			}
			if !ok {
				continue
			}

			// Always retain every fired subscription so LatestNotifiedRate advances.
			// This prevents re-firing on unchanged rates (highest-impact invariant).
			sub.LatestNotifiedRate = currentValue
			if retainErr := a.rateUserSubscriptionRepository.RetainRateUserSubscription(ctx, &sub); retainErr != nil {
				errs[source.Name] = errors.Join(errs[source.Name], errors.Join(retainErr, internal.NewTraceError()))
			}

			if sub.UserType != domain.UserTypeTelegram {
				errs[source.Name] = errors.Join(errs[source.Name], fmt.Errorf("unsupported user type: %s", sub.UserType))
				continue
			}

			buckets := userBuckets[sub.UserID]
			if buckets == nil {
				buckets = make(map[string]*dedupBucket)
				userBuckets[sub.UserID] = buckets
			}
			b := buckets[key]
			if b == nil {
				// First subscription on this dedup key: initialise the alert fields.
				// Delta and CurrentPrice are identical across all subs on the same key
				// because they all see the same currentValue in the same run.
				b = &dedupBucket{
					a: alert{
						SourceName:    source.Name,
						SourceTitle:   source.Title,
						BaseCurrency:  source.BaseCurrency,
						QuoteCurrency: source.QuoteCurrency,
						CurrencyKind:  source.Kind,
						CurrentPrice:  currentValue,
						Delta:         delta,
						Timestamp:     now,
					},
					triggers: make(map[domain.SubscriptionConditionType]string),
				}
				buckets[key] = b
			}
			b.triggers[sub.ConditionType] = collapseConditionValue(
				b.triggers[sub.ConditionType],
				sub.ConditionValue,
				sub.ConditionType,
			)
		}

		// Flatten buckets into telegramBotAlerts with triggers in canonical order.
		for userID, buckets := range userBuckets {
			for _, b := range buckets {
				b.a.Triggers = sortedTriggers(b.triggers)
				telegramBotAlerts[userID] = append(telegramBotAlerts[userID], b.a)
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

// triggerOrder defines the canonical sort position for each condition type.
var triggerOrder = map[domain.SubscriptionConditionType]int{
	domain.ConditionTypeDelta:    0,
	domain.ConditionTypeInterval: 1,
	domain.ConditionTypeDaily:    2,
	domain.ConditionTypeCron:     3,
}

// sortedTriggers converts a collapsed triggers map into a slice in canonical order
// (delta, interval, daily, cron).
func sortedTriggers(m map[domain.SubscriptionConditionType]string) []alertTrigger {
	result := make([]alertTrigger, 0, len(m))
	for ct, cv := range m {
		result = append(result, alertTrigger{ConditionType: ct, ConditionValue: cv})
	}
	sort.Slice(result, func(i, j int) bool {
		oi := triggerOrder[result[i].ConditionType]
		oj := triggerOrder[result[j].ConditionType]
		return oi < oj
	})
	return result
}

// collapseConditionValue merges an incoming condValue into an existing one per the
// most-specific-wins rule for each condition type:
//   - delta:    lowest numeric threshold (most sensitive).
//   - interval: shortest time.Duration.
//   - daily:    lexicographically-earliest HH:MM:SS.
//   - cron:     smallest weekday digit in parts[4].
//
// If existing is empty the incoming value wins unconditionally.
// On parse failure the existing entry is preferred to avoid silent regressions.
func collapseConditionValue(existing, incoming string, condType domain.SubscriptionConditionType) string {
	if existing == "" {
		return incoming
	}
	switch condType {
	case domain.ConditionTypeDelta:
		eVal, err := strconv.ParseFloat(existing, 64)
		if err != nil {
			return existing
		}
		iVal, err := strconv.ParseFloat(incoming, 64)
		if err != nil {
			return existing
		}
		if iVal < eVal {
			return incoming
		}
		return existing

	case domain.ConditionTypeInterval:
		eDur, err := time.ParseDuration(existing)
		if err != nil {
			return existing
		}
		iDur, err := time.ParseDuration(incoming)
		if err != nil {
			return existing
		}
		if iDur < eDur {
			return incoming
		}
		return existing

	case domain.ConditionTypeDaily:
		if incoming < existing {
			return incoming
		}
		return existing

	case domain.ConditionTypeCron:
		eDay := cronWeekdayDigit(existing)
		if eDay < 0 {
			return existing
		}
		iDay := cronWeekdayDigit(incoming)
		if iDay < 0 {
			return existing
		}
		if iDay < eDay {
			return incoming
		}
		return existing

	default:
		return existing
	}
}

// cronWeekdayDigit parses the weekday field (parts[4]) of a 5-field cron expression
// and returns the digit as int. Returns -1 on parse failure.
func cronWeekdayDigit(expr string) int {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return -1
	}
	d, err := strconv.Atoi(parts[4])
	if err != nil {
		return -1
	}
	return d
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
