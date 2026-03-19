package rate

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
	integration "github.com/seilbekskindirov/monitor/internal/infrastructure/telegrambot"
	"github.com/seilbekskindirov/monitor/internal/service/rateextractor"
)

func NewAgent(
	proxyURL string,
	cltTelegram telegramClient,
	rRateSource rateSourceRepository,
	rExecutionHistory executionHistoryRepository,
	rRateValue rateValueRepository,
	rRateUserSubscription rateUserSubscriptionRepository,
	logger io.Writer,
) (*Agent, error) {
	extractor, err := rateextractor.NewRateExtractor(rRateValue, proxyURL, time.Minute)
	if err != nil {
		return nil, err
	}

	a := &Agent{
		telegramClient:                 cltTelegram,
		rateValueRepository:            rRateValue,
		rateSourceRepository:           rRateSource,
		executionHistoryRepository:     rExecutionHistory,
		rateUserSubscriptionRepository: rRateUserSubscription,
		rateExtractor:                  extractor,
		logger:                         logger,
	}
	return a, nil
}

type Agent struct {
	telegramClient                 telegramClient
	rateValueRepository            rateValueRepository
	rateSourceRepository           rateSourceRepository
	executionHistoryRepository     executionHistoryRepository
	rateUserSubscriptionRepository rateUserSubscriptionRepository
	rateExtractor                  rateExtractor
	logger                         io.Writer
}

func (a *Agent) Run(ctx context.Context) (err error) {
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

func (a *Agent) execution(ctx context.Context, sources []domain.RateSource) map[string]error {
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

func (a *Agent) notification(ctx context.Context, sources []domain.RateSource) map[string]error {
	now := time.Now().UTC()
	errs := make(map[string]error, len(sources))

	telegramBotAlerts := make(map[int64][]alert, len(sources))

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
				chatID, errChatID := strconv.ParseInt(subscription.UserID, 10, 64)
				if errChatID != nil {
					errChatID = errors.Join(errChatID, internal.NewTraceError())
					errChatID = errors.Join(fmt.Errorf("ChatID %s is invalid", subscription.UserID), errChatID)
					errs[source.Name] = errChatID
					continue
				}
				items, ok := telegramBotAlerts[chatID]
				if !ok || items == nil {
					items = make([]alert, 0)
				}
				telegramBotAlerts[chatID] = append(items, alert{
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
		res := "OK"
		if err := tbotSendHTMLMessage(a.telegramClient, ctx, integration.TelegramChatID(tbotChatID), tbotAlerts); err != nil {
			res = err.Error()
		}
		log.Printf("notification: telegrambot %d: %s\n", tbotChatID, res)
	}

	return errs
}

type rateExtractor interface {
	Run(context.Context, *domain.RateSource) error
}

type executionHistoryRepository interface {
	RetainExecutionHistory(context.Context, *domain.ExecutionHistory) error
	ObtainLastNExecutionHistoryBySourceName(context.Context, string, int, bool) ([]domain.ExecutionHistory, error)
}

type rateSourceRepository interface {
	ObtainRateSourceByName(context.Context, string) (*domain.RateSource, error)
	ObtainAllRateSources(context.Context) ([]domain.RateSource, error)
}

type rateValueRepository interface {
	ObtainLastNRateValuesBySourceName(context.Context, string, int) ([]domain.RateValue, error)
	RetainRateValue(context.Context, *domain.RateValue) error
}

type rateUserSubscriptionRepository interface {
	ObtainRateUserSubscriptionsBySource(context.Context, string) ([]domain.RateUserSubscription, error)
}

type telegramClient interface {
	SendHTMLMessageToAdmin(context.Context, string) error
	SendDocumentToAdmin(context.Context, string, []byte) error
	SendHTMLMessage(context.Context, integration.TelegramChatID, string) error
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

// isDue returns true if the source should run in this invocation.
// A 30-second grace period accounts for cron scheduling jitter.
// If no successful execution history exists, the source is always considered due.
func isDue(
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

func tbotSendHTMLMessage(tbot telegramClient, ctx context.Context, chatID integration.TelegramChatID, alerts []alert) error {
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

		values, ok := rates[key]
		if !ok {
			values = make([]string, 0)
		}
		values = append(values, val)
		sort.Strings(values)
		rates[key] = values
	}

	messages := make([]string, 0, len(rates))
	for title, values := range rates {
		messages = append(messages, fmt.Sprintf("%s:\n%s\n", title, strings.Join(values, "\n\n")))
	}
	sort.Strings(messages)

	now := time.Now().UTC().Format(time.RFC850)
	errs := make([]error, 0)

	var b strings.Builder

	for _, message := range messages {
		if l := b.Len() + len(message); l < telegramMaxMessageLen {
			b.WriteString(message)
			continue
		}

		msg := b.String()
		b.Reset()

		err := tbot.SendHTMLMessage(ctx, chatID, fmt.Sprintf("#COLLECTOR %s\n%s", now, msg))
		if err != nil {
			errs = append(errs, err)
		}
	}

	msg := b.String()
	b.Reset()

	if len(msg) > 0 {
		err := tbot.SendHTMLMessage(ctx, chatID, fmt.Sprintf("#COLLECTOR %s\n%s", now, msg))
		if err != nil {
			errs = append(errs, err)
		}
	}

	if err := errors.Join(errs...); err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	return nil
}
