package notification

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
)

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
	if logger == nil {
		logger = io.Discard
	}
	return &RateCheckAgent{
		rateSourceRepository:           rRateSource,
		rateValueRepository:            rRateValue,
		rateUserSubscriptionRepository: rRateUserSubscription,
		rateUserEventRepository:        rRateUserEvent,
		logger:                         logger,
	}, nil
}

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

// Run iterates every rate source, checks active subscriptions, and queues alert events
// for any subscription whose notification condition is currently satisfied.
//
// Cross-source dedup: subscriptions on the same (base, quote, kind) key for the same
// user produce a single table row regardless of which source they come from. The
// displayed price is the extremum across sources (BID→MAX, ASK→MIN) and the delta
// always comes from the same source as the winning price. Every fired subscription is
// still retained individually so its LatestNotifiedRate advances and it does not re-fire.
func (a *RateCheckAgent) Run(ctx context.Context) error {
	sources, err := a.rateSourceRepository.ObtainAllRateSources(ctx)
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}
	if len(sources) == 0 {
		return nil
	}

	now := time.Now().UTC()
	errs := make(map[string]error, len(sources))

	// userBuckets is hoisted outside the source loop so buckets accumulate
	// across all sources before being flattened into alerts.
	// Structure: userID → dedupKey → *dedupBucket.
	// dedupKey = base|quote|kind (source name intentionally excluded).
	userBuckets := make(map[string]map[string]*dedupBucket)

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

		// Dedup key omits source.Name so the same pair+kind at multiple sources
		// folds into one row per user.
		key := source.BaseCurrency + "|" + source.QuoteCurrency + "|" + string(source.Kind)

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
			// Retain is unconditional and separate from the bucket-fold logic below
			// so a sub that collapses into an existing bucket still advances.
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
				// First source to contribute to this key: initialise the bucket.
				b = &dedupBucket{
					a: alert{
						SourceName:    source.Name,
						BaseCurrency:  source.BaseCurrency,
						QuoteCurrency: source.QuoteCurrency,
						CurrencyKind:  source.Kind,
						CurrentPrice:  currentValue,
						Delta:         delta,
					},
					triggers: make(map[domain.SubscriptionConditionType]string),
				}
				buckets[key] = b
			} else {
				// A bucket for this key already exists (possibly from a different
				// source). Apply extremum selection: BID keeps the MAX price,
				// ASK keeps the MIN price. Price and delta are replaced together
				// so they always come from the same source (coherence invariant).
				// Strict > (BID) / < (ASK): on equal prices the first-seen source wins.
				switch source.Kind {
				case domain.RateSourceKindBID:
					if currentValue > b.a.CurrentPrice {
						b.a.CurrentPrice = currentValue
						b.a.Delta = delta
						b.a.SourceName = source.Name
					}
				case domain.RateSourceKindASK:
					if currentValue < b.a.CurrentPrice {
						b.a.CurrentPrice = currentValue
						b.a.Delta = delta
						b.a.SourceName = source.Name
					}
				}
			}
			// Trigger accumulation continues regardless of which source holds the
			// winning price — we collect reason bits from all contributing sources.
			b.triggers[sub.ConditionType] = collapseConditionValue(
				b.triggers[sub.ConditionType],
				sub.ConditionValue,
				sub.ConditionType,
			)
		}
	}

	// Flatten all user buckets into telegramBotAlerts after processing all sources.
	// This must run outside the source loop so cross-source extremum selection is
	// complete before alerts are built.
	telegramBotAlerts := make(map[string][]alert, len(userBuckets))
	for userID, buckets := range userBuckets {
		for _, b := range buckets {
			b.a.Triggers = sortedTriggers(b.triggers)
			telegramBotAlerts[userID] = append(telegramBotAlerts[userID], b.a)
		}
	}

	combined := make([]error, 0, len(errs)+len(telegramBotAlerts))
	for k, e := range errs {
		if e != nil {
			combined = append(combined, fmt.Errorf("source %s: %w", k, e))
		}
	}

	var (
		totalChats     int
		totalQueued    int
		totalAttempted int
	)
	for chatID, alerts := range telegramBotAlerts {
		msgs, err := buildAlertMessage(now, alerts...)
		if err != nil {
			// Returned to caller via combined; cmd/notifier logs the joined error
			// at run completion. Avoid logging here too — duplicate noise.
			combined = append(combined, fmt.Errorf("build message for chat_id=%s: %w", chatID, err))
			continue
		}
		// Bind the event to a source when this delivery covers exactly one source;
		// when alerts span multiple sources we leave SourceName empty (→ NULL via
		// sourceNameForDB) so /events/failed by source isn't mislabelled.
		var eventSourceName string
		if singleSource := singleSourceName(alerts); singleSource != "" {
			eventSourceName = singleSource
		}
		var failCount int
		for _, msg := range msgs {
			if retainErr := a.rateUserEventRepository.RetainRateUserEvent(ctx, &domain.RateUserEvent{
				UserType:   domain.UserTypeTelegram,
				UserID:     chatID,
				Message:    msg,
				SourceName: eventSourceName,
			}); retainErr != nil {
				failCount++
				combined = append(combined, fmt.Errorf("retain event chat_id=%s: %w", chatID, retainErr))
			}
		}
		queued := len(msgs) - failCount
		if queued < 0 {
			queued = 0 // defensive — failCount is bounded by len(msgs) but the
			// invariant is not enforced by the type system.
		}
		totalChats++
		totalQueued += queued
		totalAttempted += len(msgs)
	}
	// Always emit a marker so operators have proof-of-execution even on
	// quiet runs. Per-chat detail lives in combined (failures) and in the
	// rate_user_events table either way. Routed through a.logger so tests
	// that construct the agent via NewRateCheckAgent(..., io.Discard) don't
	// pollute the global logger; tests that build the struct literal directly
	// and leave logger nil fall back to io.Discard here.
	logger := a.logger
	if logger == nil {
		logger = io.Discard
	}
	fmt.Fprintf(logger, "notification check: queued %d/%d events across %d chats\n",
		totalQueued, totalAttempted, totalChats)

	return errors.Join(combined...)
}

// triggerOrder defines the canonical sort position for each condition type.
var triggerOrder = map[domain.SubscriptionConditionType]int{
	domain.ConditionTypeDelta:    0,
	domain.ConditionTypeInterval: 1,
	domain.ConditionTypeDaily:    2,
	domain.ConditionTypeCron:     3,
}

// dedupBucket holds a single alert and the collapsed trigger values for each condition
// type that has fired on the same (base, quote, kind) key for one user.
// The bucket may accumulate contributions from multiple sources when cross-source dedup
// is active; the alert's price+delta always belong to the single winning source.
type dedupBucket struct {
	a        alert
	triggers map[domain.SubscriptionConditionType]string
}

// singleSourceName returns the SourceName shared by all alerts in the slice,
// or the empty string if alerts span multiple sources (or the slice is empty).
// Used to attribute a notification event to its source when delivery is
// 1:1 with a source, and leave attribution empty when one Telegram message
// collapses alerts from several sources.
func singleSourceName(alerts []alert) string {
	if len(alerts) == 0 {
		return ""
	}
	name := alerts[0].SourceName
	for _, a := range alerts[1:] {
		if a.SourceName != name {
			return ""
		}
	}
	return name
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
