package integration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/prorochestvo/dsninjector"
	"github.com/seilbekskindirov/monitor/internal"
)

// UpdateHandler is called for every incoming Telegram update in the event bus.
type UpdateHandler func(ctx context.Context, update tgbotapi.Update)
type TelegramChatID int64

func NewTBotClient(tbotDSN dsninjector.DataSource, logger io.Writer) (*TelegramBotClient, error) {
	rx := regexp.MustCompile(regexpTelegramToken)

	token := strings.TrimSpace(tbotDSN.Addr())
	if token == "" || rx.MatchString(token) == false {
		return nil, errors.New("telegram: bot token is required")
	}

	adminChatID, err := strconv.ParseInt(tbotDSN.Login(), 10, 64)
	if err != nil || adminChatID == 0 {
		if err == nil {
			err = fmt.Errorf("admin chat id cannot be zero")
		}
		err = fmt.Errorf("invalid admin chat id: %w", err)
		return nil, errors.Join(err, internal.NewTraceError())
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("telegram: init bot: %w", err)
	}

	bot.Debug = false

	t := &TelegramBotClient{
		bot:         bot,
		adminChatID: TelegramChatID(adminChatID),
		logger:      logger,
	}

	return t, nil
}

type TelegramBotClient struct {
	bot         *tgbotapi.BotAPI
	adminChatID TelegramChatID
	logger      io.Writer
}

func (tbot *TelegramBotClient) Ping(_ context.Context) error {
	u, err := tbot.bot.GetMe()
	if err != nil {
		return errors.Join(err, internal.NewTraceError())
	}
	if u.ID == 0 {
		return errors.New("telegram: bot ping failed: invalid response")
	}
	return nil
}

func (tbot *TelegramBotClient) Me(_ context.Context) (TelegramChatID, string, error) {
	u, err := tbot.bot.GetMe()
	if err != nil {
		return 0, "", errors.Join(err, internal.NewTraceError())
	}
	if u.ID == 0 {
		return 0, "", errors.New("telegram: bot ping failed: invalid response")
	}
	return TelegramChatID(u.ID), u.UserName, nil
}

func (tbot *TelegramBotClient) SendHTMLMessageToAdmin(ctx context.Context, text string) error {
	return tbot.SendHTMLMessage(ctx, tbot.adminChatID, text)
}

func (tbot *TelegramBotClient) SendHTMLMessage(_ context.Context, chatID TelegramChatID, text string) error {
	m := tgbotapi.NewMessage(int64(chatID), text)
	m.ParseMode = tgbotapi.ModeHTML

	if _, err := tbot.bot.Send(m); err != nil {
		return errors.Join(err, internal.NewTraceError())
	}
	return nil
}

func (tbot *TelegramBotClient) SendDocumentToAdmin(ctx context.Context, fileName string, fileContent []byte) error {
	return tbot.SendDocument(ctx, tbot.adminChatID, fileName, fileContent)
}

// SendDocument uploads content as a file named name to chatID.
func (tbot *TelegramBotClient) SendDocument(_ context.Context, chatID TelegramChatID, fileName string, fileContent []byte) error {
	doc := tgbotapi.NewDocument(int64(chatID), tgbotapi.FileBytes{Name: fileName, Bytes: fileContent})
	if _, err := tbot.bot.Send(doc); err != nil {
		return errors.Join(err, internal.NewTraceError())
	}
	return nil
}

// Listen starts long-polling and dispatches every incoming update to handler.
// Blocks until ctx is cancelled — run it in a goroutine.
func (tbot *TelegramBotClient) Listen(ctx context.Context, handler UpdateHandler) {
	log.Println("telegram: bot started listening for updates")
	defer log.Println("telegram: bot stopped listening for updates")

	isTerminated := false

	cfg := tgbotapi.NewUpdate(0)
	cfg.Timeout = 30
	updates := tbot.bot.GetUpdatesChan(cfg)

	for !isTerminated {
		select {
		case <-ctx.Done():
			tbot.bot.StopReceivingUpdates()
			isTerminated = true
		case update, ok := <-updates:
			if !ok {
				return
			}
			handler(ctx, update)
		}
	}
}

const regexpTelegramToken = `^\d{9,}:[a-zA-Z0-9_-]{35,}$`
