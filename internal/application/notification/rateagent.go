package notification

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	integration "github.com/seilbekskindirov/monitor/internal/infrastructure/telegrambot"
)

func NewRateAgent(
	cltTelegram telegramClient,
	rRateUserEvent rateUserEventRepository,
) (*RateAgent, error) {

	a := &RateAgent{
		rateUserEventRepository: rRateUserEvent,
		telegramClient:          cltTelegram,
		ttl:                     24 * time.Hour,
	}

	return a, nil
}

type RateAgent struct {
	telegramClient          telegramClient
	rateUserEventRepository rateUserEventRepository
	ttl                     time.Duration
}

func (a *RateAgent) Run(ctx context.Context) error {
	events, err := a.rateUserEventRepository.ObtainUnprocessedRateUserEvents(ctx)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	var errs []error

	now := time.Now().UTC()
	ttl := now.Add(-a.ttl)

	for _, event := range events {
		if event.CreatedAt.Before(ttl) {
			event.Status = domain.RateUserEventStatusCanceled
			event.SentAt = time.Time{}
			event.LastError = strings.Join([]string{fmt.Sprintf("TTL (%s) exceeded", a.ttl.String()), event.LastError}, "\n")
		} else {
			switch event.UserType {
			case domain.UserTypeTelegram:
				err = a.runUserTypeTelegram(ctx, &event)
			default:
				err = fmt.Errorf("unsupported user type: %s", event.UserType)
			}
			if err != nil {
				event.LastError = err.Error()
				event.Status = domain.RateUserEventStatusFailed
				event.SentAt = time.Time{}
				errs = append(errs, errors.Join(err, internal.NewTraceError()))
			} else {
				event.Status = domain.RateUserEventStatusSent
				event.LastError = ""
				event.SentAt = now
			}
		}

		err = a.rateUserEventRepository.RetainRateUserEvent(ctx, &event)
		if err != nil {
			errs = append(errs, errors.Join(err, internal.NewTraceError()))
		}

		// delay for avoid hitting Telegram rate limits in case of many pending notifications
		time.Sleep(500 * time.Millisecond)
	}

	return errors.Join(errs...)
}

func (a *RateAgent) runUserTypeTelegram(ctx context.Context, event *domain.RateUserEvent) error {
	if event == nil {
		err := errors.New("notification record is nil")
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	chatID, err := strconv.ParseInt(event.UserID, 10, 64)
	if err != nil || chatID == 0 {
		if err == nil {
			err = fmt.Errorf("invalid user id: %s", event.UserID)
		}
		err = errors.Join(fmt.Errorf("invalid Telegram chat ID: %s", event.UserID), err)
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	err = a.telegramClient.SendHTMLMessage(ctx, integration.TelegramChatID(chatID), event.Message)
	if err != nil {
		err = errors.Join(err, internal.NewTraceError())
		return err
	}

	return nil
}

// Vacuum removes all non-pending records older than 180 days.
func (a *RateAgent) Vacuum(ctx context.Context) error {
	return a.rateUserEventRepository.RemoveRateUserEventOlderThan(ctx, 180*24*time.Hour)
}

// rateUserEventRepository is the narrow storage interface required by this service.
type rateUserEventRepository interface {
	ObtainUnprocessedRateUserEvents(context.Context) ([]domain.RateUserEvent, error)
	RetainRateUserEvent(context.Context, *domain.RateUserEvent) error
	RemoveRateUserEventOlderThan(ctx context.Context, duration time.Duration) error
}

// telegramClient is the narrow Telegram transport interface required by this service.
type telegramClient interface {
	SendHTMLMessage(context.Context, integration.TelegramChatID, string) error
}
