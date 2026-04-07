package collection

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/service/rateextractor"
)

func NewRateAgent(
	proxyURL string,
	rRateSource rateSourceRepository,
	rExecutionHistory executionHistoryRepository,
	rRateValue rateValueRepository,
	rRateUserSubscription rateUserSubscriptionRepository,
	rRateUserEvent rateUserEventRepository,
	logger io.Writer,
) (*RateAgent, error) {
	extractor, err := rateextractor.NewRateExtractor(rRateValue, proxyURL, time.Minute)
	if err != nil {
		return nil, err
	}

	a := &RateAgent{
		rateValueRepository:            rRateValue,
		rateSourceRepository:           rRateSource,
		executionHistoryRepository:     rExecutionHistory,
		rateUserSubscriptionRepository: rRateUserSubscription,
		rateUserEventRepository:        rRateUserEvent,
		rateExtractor:                  extractor,
		logger:                         logger,
	}

	return a, nil
}

type RateAgent struct {
	rateValueRepository            rateValueRepository
	rateSourceRepository           rateSourceRepository
	executionHistoryRepository     executionHistoryRepository
	rateUserSubscriptionRepository rateUserSubscriptionRepository
	rateUserEventRepository        rateUserEventRepository
	rateExtractor                  rateExtractor
	logger                         io.Writer
}

func (a *RateAgent) Run(ctx context.Context) (err error) {
	// isDue returns true if the source should run in this invocation.
	// A 30-second grace period accounts for cron scheduling jitter.
	// If no successful execution history exists, the source is always considered due.
	isDue := func(
		ctx context.Context,
		repository executionHistoryRepository,
		sourceName string,
		interval time.Duration,
		now time.Time,
	) bool {
		records, err := repository.ObtainLastNExecutionHistoryBySourceName(ctx, sourceName, 1, true)
		if err != nil || len(records) == 0 {
			return true
		}
		return now.Sub(records[0].Timestamp) >= interval-30*time.Second
	}

	var sources []domain.RateSource
	if s, errSource := a.rateSourceRepository.ObtainAllRateSources(ctx); errSource != nil {
		errSource = errors.Join(errSource, internal.NewTraceError())
		return errSource
	} else if len(s) > 0 {
		now := time.Now().UTC()
		sources = make([]domain.RateSource, 0, len(s))
		for _, source := range s {
			interval, errInterval := time.ParseDuration(source.Interval)
			if errInterval != nil {
				errInterval = fmt.Errorf("invalid interval %q, %s", source.Interval, errInterval.Error())
				errInterval = errors.Join(errInterval, internal.NewTraceError())
				err = errors.Join(err, errInterval)
				continue
			}
			if !isDue(ctx, a.executionHistoryRepository, source.Name, interval, now) {
				continue
			}
			sources = append(sources, source)
		}
	}

	if sources == nil || len(sources) == 0 {
		return
	}

	errExecution := a.execution(ctx, sources)
	errNotification := a.notification(ctx, sources)

	l := len(errExecution)
	if extra := len(errNotification); extra > l {
		l = extra
	}

	m := make(map[string]error, l)
	for k, e := range errExecution {
		err, ok := m[k]
		if !ok {
			err = nil
		}
		m[k] = errors.Join(err, e)
	}
	for k, e := range errNotification {
		err, ok := m[k]
		if !ok {
			err = nil
		}
		m[k] = errors.Join(err, e)
	}

	errs := make([]error, 0, len(m))
	for k, e := range m {
		if e == nil {
			continue
		}
		errs = append(errs, fmt.Errorf("source %s: %s", k, e.Error()))
	}

	err = errors.Join(err, errors.Join(errs...))

	return
}

func (a *RateAgent) execution(ctx context.Context, sources []domain.RateSource) map[string]error {
	now := time.Now().UTC()
	errs := make(map[string]error, len(sources))

	for _, source := range sources {
		h := &domain.ExecutionHistory{
			SourceName: source.Name,
			Success:    true,
			Timestamp:  now,
		}

		err := a.rateExtractor.Run(ctx, &source)
		if err != nil {
			h.Success = false
			h.Error = errors.Join(err, internal.NewTraceError()).Error()
		}

		err = errors.Join(err, a.executionHistoryRepository.RetainExecutionHistory(ctx, h))
		if err != nil {
			errs[source.Name] = err
		}
	}

	return errs
}

func (a *RateAgent) notification(ctx context.Context, sources []domain.RateSource) map[string]error {
	now := time.Now().UTC()
	errs := make(map[string]error, len(sources))

	telegramBotAlerts := make(map[string][]alert, len(sources))

	for _, source := range sources {
		var newValue float64
		var oldValue float64

		if values, err := a.rateValueRepository.ObtainLastNRateValuesBySourceName(ctx, source.Name, 2); err != nil {
			err = errors.Join(err, internal.NewTraceError())
			errs[source.Name] = err
			continue
		} else if l := len(values); l == 0 {
			continue
		} else if l >= 2 {
			newValue = values[0].Price
			oldValue = values[1].Price
		} else if l >= 1 {
			newValue = values[0].Price
			oldValue = 0.0
		} else {
			continue
		}

		subscriptions, err := a.rateUserSubscriptionRepository.ObtainRateUserSubscriptionsBySource(ctx, source.Name)
		if err != nil {
			err = errors.Join(err, internal.NewTraceError())
			errs[source.Name] = err
			continue
		}

		for _, subscription := range subscriptions {
			switch subscription.UserType {
			case domain.UserTypeTelegram:
				items, ok := telegramBotAlerts[subscription.UserID]
				if !ok || items == nil {
					items = make([]alert, 0)
				}
				telegramBotAlerts[subscription.UserID] = append(items, alert{
					SourceName:     source.Name,
					SourceTitle:    source.Title,
					BaseCurrency:   source.BaseCurrency,
					QuoteCurrency:  source.QuoteCurrency,
					CurrentPrice:   newValue,
					Delta:          newValue - oldValue,
					ForecastPrice:  0.0,
					ForecastMethod: "",
					Timestamp:      now,
				})
			default:
				errs[source.Name] = fmt.Errorf("unsupported user type: %s", subscription.UserType)
			}
		}
	}

	for tbotChatID, tbotAlerts := range telegramBotAlerts {
		var errMessages []error

		msgs, err := buildAlertMessage(tbotAlerts...)
		if err == nil {
			errMessages = make([]error, 0, len(msgs))
			for _, msg := range msgs {
				err = a.rateUserEventRepository.RetainRateUserEvent(ctx, &domain.RateUserEvent{
					UserType: domain.UserTypeTelegram,
					UserID:   tbotChatID,
					Message:  msg,
				})
				if err != nil {
					errMessages = append(errMessages, err)
				}
			}
			err = errors.Join(errMessages...)
		}

		res := ""
		if err != nil {
			res = " " + err.Error()
		}

		log.Printf("notification: telegram chat_id=%s queued: %d/%d%s", tbotChatID, len(msgs)-len(errMessages), len(msgs), res)
	}

	return errs
}

type rateExtractor interface {
	Run(context.Context, *domain.RateSource) error
}

type executionHistoryRepository interface {
	RetainExecutionHistory(context.Context, *domain.ExecutionHistory) error
	ObtainLastNExecutionHistoryBySourceName(context.Context, string, int64, bool) ([]domain.ExecutionHistory, error)
}

type rateSourceRepository interface {
	ObtainRateSourceByName(context.Context, string) (*domain.RateSource, error)
	ObtainAllRateSources(context.Context) ([]domain.RateSource, error)
}

type rateValueRepository interface {
	ObtainLastNRateValuesBySourceName(context.Context, string, int64) ([]domain.RateValue, error)
	RetainRateValue(context.Context, *domain.RateValue) error
}

type rateUserSubscriptionRepository interface {
	ObtainRateUserSubscriptionsBySource(context.Context, string) ([]domain.RateUserSubscription, error)
}

type rateUserEventRepository interface {
	RetainRateUserEvent(ctx context.Context, record *domain.RateUserEvent) error
}

type alert struct {
	UserID         string
	SourceName     string
	SourceTitle    string    // human-readable source name, e.g. "National Bank of Kazakhstan"
	BaseCurrency   string    // e.g. "USD"
	QuoteCurrency  string    // e.g. "KZT"
	CurrentPrice   float64   // newest price, e.g. 470.46
	Delta          float64   // signed delta: positive = up, negative = down
	ForecastPrice  float64   //
	ForecastMethod string    //
	Timestamp      time.Time // timestamp of the newest rate record
}

// https://apps.timwhitlock.info/emoji/tables/unicode
const (
	telegramBotArrowUp   string = "🔼"
	telegramBotArrowDown string = "🔽"
	telegramBotForecast  string = "✨"

	telegramMaxMessageLen = 2048
)

// buildAlertMessage renders alerts into the builder as a single HTML Telegram message.
func buildAlertMessage(alerts ...alert) ([]string, error) {
	rates := make(map[string][]string, len(alerts))
	for _, alertItem := range alerts {
		key := strings.TrimSpace(alertItem.SourceTitle)
		val := fmt.Sprintf(" • <b>%s/%s</b>: %.2f", alertItem.BaseCurrency, alertItem.QuoteCurrency, alertItem.CurrentPrice)
		if alertItem.Delta != 0 && alertItem.Delta != alertItem.CurrentPrice {
			arrow := telegramBotArrowUp
			if alertItem.Delta < 0 {
				arrow = telegramBotArrowDown
			}
			val += fmt.Sprintf(" (%.2f %s)", alertItem.Delta, arrow)
		}
		if alertItem.ForecastMethod != "" && alertItem.ForecastPrice != 0.0 {
			val += fmt.Sprintf(" | %s %.2f <i>%s</i>", telegramBotForecast, alertItem.ForecastPrice, alertItem.ForecastMethod)
		}
		values := rates[key]
		values = append(values, val)
		sort.Strings(values)
		rates[key] = values
	}

	sources := make([]string, 0, len(rates))
	for title, values := range rates {
		sources = append(sources, fmt.Sprintf("%s:\n%s\n", title, strings.Join(values, "\n\n")))
	}
	sort.Strings(sources)

	now := time.Now().UTC().Format(time.RFC850)

	var buffer strings.Builder
	messages := make([]string, 0, len(sources))
	for _, message := range sources {
		if l := buffer.Len() + len(message); l < telegramMaxMessageLen {
			buffer.WriteString(message)
			continue
		}
		messages = append(messages, fmt.Sprintf("#COLLECTOR %s\n%s", now, buffer.String()))
		buffer.Reset()
	}
	if buffer.Len() > 0 {
		messages = append(messages, fmt.Sprintf("#COLLECTOR %s\n%s", now, buffer.String()))
		buffer.Reset()
	}

	return messages, nil
}
