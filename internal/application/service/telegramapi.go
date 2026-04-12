package service

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/seilbekskindirov/monitor/internal/domain"
	integration "github.com/seilbekskindirov/monitor/internal/infrastructure/telegrambot"
)

// NewTelegramApi constructs a fully stateless handler. All three arguments are required.
func NewTelegramApi(
	cltTelegram telegramClient,
	subRepo subscriptionRepository,
	sourceRepo sourceRepository,
) (*TelegramApi, error) {
	return &TelegramApi{
		telegramClient: cltTelegram,
		subRepo:        subRepo,
		sourceRepo:     sourceRepo,
	}, nil
}

// TelegramApi implements Telegram bot subscription CRUD via inline keyboards.
// It is fully stateless — all flow context travels inside callback_data.
type TelegramApi struct {
	telegramClient telegramClient
	subRepo        subscriptionRepository
	sourceRepo     sourceRepository
}

func (h *TelegramApi) Run(ctx context.Context) {
	handle := func(ctx context.Context, update tgbotapi.Update) {
		switch {
		case update.CallbackQuery != nil:
			h.handleCallback(ctx, update.CallbackQuery)
		case update.Message != nil:
			h.handleMessage(ctx, update.Message)
		}
	}

	go h.telegramClient.Listen(ctx, handle)
}

// handleMessage routes commands to the main menu; all other text is ignored.
func (h *TelegramApi) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	lower := strings.TrimSpace(strings.ToLower(msg.Text))

	if lower == commandSubscriptions || lower == commandStart {
		h.sendMainMenu(ctx, chatID)
		return
	}

	_ = h.telegramClient.SendHTMLMessage(ctx,
		integration.TelegramChatID(chatID),
		fmt.Sprintf("Please use %s to start.", commandSubscriptions))
}

// handleCallback routes inline-keyboard presses to the correct handler.
//
// Add-flow callback_data layout (all segments after "sub:add:"):
//
//	sub:add:<src>               — source chosen → show condition types
//	sub:add:<src>:delta         — delta chosen  → show delta value buttons
//	sub:add:<src>:interval      — interval chosen → show interval value buttons
//	sub:add:<src>:delta:<val>   — value chosen  → save subscription
//	sub:add:<src>:interval:<val>— value chosen  → save subscription
//
// Delete-flow:
//
//	sub:del:<src>               — show confirm dialog
//	sub:del:yes:<src>           — confirmed → delete
func (h *TelegramApi) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
	chatID := cb.Message.Chat.ID
	msgID := cb.Message.MessageID
	data := cb.Data

	// always acknowledge to clear the spinner
	_ = h.telegramClient.AnswerCallbackQuery(ctx, cb.ID, "")

	switch {
	case data == cbBack:
		h.sendMainMenu(ctx, chatID)

	case data == cbShow:
		h.handleShow(ctx, chatID, msgID)

	case data == cbLatest:
		h.handleLatestUpdates(ctx, chatID)

	case data == cbAdd:
		h.handleAddSourceList(ctx, chatID)

	case strings.HasPrefix(data, cbAddSrcPrefix):
		h.routeAddFlow(ctx, chatID, strings.TrimPrefix(data, cbAddSrcPrefix))

	case data == cbDelete:
		h.handleDeleteList(ctx, chatID, msgID)

	// sub:del:yes: must be checked before sub:del: — the latter is a prefix of the former
	case strings.HasPrefix(data, "sub:del:yes:"):
		sourceName, _ := url.QueryUnescape(strings.TrimPrefix(data, "sub:del:yes:"))
		h.handleDeleteConfirm(ctx, chatID, sourceName)

	case strings.HasPrefix(data, "sub:del:"):
		sourceName, _ := url.QueryUnescape(strings.TrimPrefix(data, "sub:del:"))
		h.handleDeleteAsk(ctx, chatID, msgID, sourceName)

	case data == cbDelNo:
		h.sendMainMenu(ctx, chatID)
	}
}

// routeAddFlow dispatches the add-subscription flow based on the number of
// decoded segments after "sub:add:".
//
//	1 segment  → source chosen
//	2 segments → condition type chosen
//	3 segments → value chosen → save
func (h *TelegramApi) routeAddFlow(ctx context.Context, chatID int64, rest string) {
	// rest examples:
	//   "Halyk%20Bank"
	//   "Halyk%20Bank:delta"
	//   "Halyk%20Bank:delta:0.5"
	//   "Halyk%20Bank:interval:30m"
	parts := strings.SplitN(rest, ":", 3)

	sourceName, _ := url.QueryUnescape(parts[0])

	switch len(parts) {
	case 1:
		// source selected → show condition types
		h.handleAddSourceSelect(ctx, chatID, sourceName)

	case 2:
		// condition type selected → show value buttons
		ct := conditionFromString(parts[1])
		if ct == "" {
			return
		}
		h.handleAddValueSelect(ctx, chatID, sourceName, ct)

	case 3:
		// value selected → save subscription
		ct := conditionFromString(parts[1])
		value := parts[2]
		if ct == "" || value == "" {
			return
		}
		h.saveSubscription(ctx, chatID, &domain.RateUserSubscription{
			UserType:       domain.UserTypeTelegram,
			UserID:         strconv.FormatInt(chatID, 10),
			SourceName:     sourceName,
			ConditionType:  ct,
			ConditionValue: value,
		})
	}
}

// sendMainMenu sends the top-level four-button inline keyboard.
// Called on /start, /subscriptions, or « Back from any sub-screen.
func (h *TelegramApi) sendMainMenu(ctx context.Context, chatID int64) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 My subscriptions", cbShow),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("➕ Add subscription", cbAdd),
			tgbotapi.NewInlineKeyboardButtonData("🗑 Delete subscription", cbDelete),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📈 Latest updates", cbLatest),
		),
	)
	_ = h.telegramClient.SendHTMLMessageWithKeyboard(
		ctx,
		integration.TelegramChatID(chatID),
		"<b>Subscription Management</b>\nChoose an action:",
		kb,
	)
}

// handleShow lists the caller's active subscriptions.
func (h *TelegramApi) handleShow(ctx context.Context, chatID int64, _ int) {
	subs, err := h.subRepo.ObtainRateUserSubscriptionsByUserID(
		ctx, domain.UserTypeTelegram, strconv.FormatInt(chatID, 10),
	)
	if err != nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Failed to load subscriptions.")
		return
	}
	if len(subs) == 0 {
		_ = h.telegramClient.SendHTMLMessageWithKeyboard(ctx,
			integration.TelegramChatID(chatID),
			"You have no active subscriptions.", backKeyboard())
		return
	}
	var sb strings.Builder
	sb.WriteString("<b>Your subscriptions:</b>\n")
	for _, s := range subs {
		sb.WriteString(fmt.Sprintf(" • <b>%s</b> — %s\n",
			s.SourceName, subscriptionConditionLabel(s)))
	}
	_ = h.telegramClient.SendHTMLMessageWithKeyboard(ctx,
		integration.TelegramChatID(chatID), sb.String(), backKeyboard())
}

// handleLatestUpdates shows the last known rate for each of the caller's subscriptions.
// It displays the rate at the time of the last notification (LatestNotifiedRate + UpdatedAt),
// not the live market rate — this is intentional for MVP.
func (h *TelegramApi) handleLatestUpdates(ctx context.Context, chatID int64) {
	subs, err := h.subRepo.ObtainRateUserSubscriptionsByUserID(
		ctx, domain.UserTypeTelegram, strconv.FormatInt(chatID, 10),
	)
	if err != nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Failed to load subscriptions.")
		return
	}
	if len(subs) == 0 {
		_ = h.telegramClient.SendHTMLMessageWithKeyboard(ctx,
			integration.TelegramChatID(chatID),
			"You have no subscriptions yet.", backKeyboard())
		return
	}
	var sb strings.Builder
	sb.WriteString("<b>Latest known rates:</b>\n")
	for _, s := range subs {
		if s.LatestNotifiedRate == 0 {
			sb.WriteString(fmt.Sprintf(" • <b>%s</b>: no data yet\n", s.SourceName))
		} else {
			sb.WriteString(fmt.Sprintf(" • <b>%s</b>: %.4f  <i>(as of %s UTC)</i>\n",
				s.SourceName,
				s.LatestNotifiedRate,
				s.UpdatedAt.UTC().Format("2006-01-02 15:04"),
			))
		}
	}
	_ = h.telegramClient.SendHTMLMessageWithKeyboard(ctx,
		integration.TelegramChatID(chatID), sb.String(), backKeyboard())
}

// handleAddSourceList fetches all rate sources and presents them as an inline keyboard.
// Callback data format: sub:add:<urlencoded_source_name>
//
// NOTE: Telegram enforces a hard 64-byte limit on callback_data. Source names longer than
// ~30 characters may exceed this limit after URL-encoding. Buttons with overlong callback
// data are skipped and a warning is printed to stderr.
func (h *TelegramApi) handleAddSourceList(ctx context.Context, chatID int64) {
	sources, err := h.sourceRepo.ObtainAllRateSources(ctx)
	if err != nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Failed to load rate sources.")
		return
	}
	if len(sources) == 0 {
		_ = h.telegramClient.SendHTMLMessageWithKeyboard(ctx,
			integration.TelegramChatID(chatID),
			"No rate sources are configured yet.", backKeyboard())
		return
	}
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(sources)+1)
	for _, src := range sources {
		data := cbAddSrcPrefix + url.QueryEscape(src.Name)
		if len(data) > maxCallbackDataBytes {
			fmt.Printf("telegramapi: skipping source %q: callback data %d bytes exceeds 64-byte limit\n",
				src.Name, len(data))
			continue
		}
		label := fmt.Sprintf("%s (%s/%s)", src.Title, src.BaseCurrency, src.QuoteCurrency)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, data),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Back", cbBack),
	))
	_ = h.telegramClient.SendHTMLMessageWithKeyboard(ctx,
		integration.TelegramChatID(chatID),
		"Choose a <b>rate source</b> to subscribe to:",
		tgbotapi.NewInlineKeyboardMarkup(rows...))
}

// handleAddSourceSelect shows the condition type buttons after a source is chosen.
// Callback data format: sub:add:<urlencoded_source>:delta or :interval
func (h *TelegramApi) handleAddSourceSelect(ctx context.Context, chatID int64, sourceName string) {
	encoded := url.QueryEscape(sourceName)
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📉 On rate delta",
				cbAddSrcPrefix+encoded+":delta"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⏰ On fixed interval",
				cbAddSrcPrefix+encoded+":interval"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🕐 Daily (UTC)",
				cbAddSrcPrefix+encoded+":daily"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📅 Weekly (UTC)",
				cbAddSrcPrefix+encoded+":cron"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("« Back", cbBack),
		),
	)
	_ = h.telegramClient.SendHTMLMessageWithKeyboard(ctx,
		integration.TelegramChatID(chatID),
		fmt.Sprintf("Choose notification condition for <b>%s</b>:", sourceName),
		kb)
}

// handleAddValueSelect presents preset value buttons for the chosen condition type.
// Callback data format: sub:add:<urlencoded_source>:<conditionType>:<value>
func (h *TelegramApi) handleAddValueSelect(
	ctx context.Context,
	chatID int64,
	sourceName string,
	ct domain.SubscriptionConditionType,
) {
	encoded := url.QueryEscape(sourceName)
	prefix := cbAddSrcPrefix + encoded + ":" + string(ct) + ":"

	var (
		prompt string
		labels []string
		values []string
	)

	switch ct {
	case domain.ConditionTypeDelta:
		prompt = fmt.Sprintf("Choose <b>delta threshold</b> for <b>%s</b>\n"+
			"(notify when rate changes by at least this percentage):", sourceName)
		labels = []string{"5%", "10%", "50%", "75%", "90%"}
		values = []string{"5", "10", "50", "75", "90"}

	case domain.ConditionTypeInterval:
		prompt = fmt.Sprintf("Choose <b>notification interval</b> for <b>%s</b>:", sourceName)
		labels = []string{"2h", "4h", "6h", "12h", "1d", "1w"}
		values = []string{"2h", "4h", "6h", "12h", "24h", "168h"}

	case domain.ConditionTypeDaily:
		// values stored as time.TimeOnly ("15:04:05") — what DailyTime() expects
		prompt = fmt.Sprintf("Choose <b>daily notification time</b> for <b>%s</b> (UTC):", sourceName)
		labels = []string{"03:00", "06:00", "09:00", "12:00", "15:00", "18:00", "21:00", "00:00"}
		values = []string{"03:00:00", "06:00:00", "09:00:00", "12:00:00", "15:00:00", "18:00:00", "21:00:00", "00:00:00"}

	case domain.ConditionTypeCron:
		// values are standard 5-field cron expressions fired every week on the chosen day at 09:00 UTC
		prompt = fmt.Sprintf("Choose <b>weekly notification day</b> for <b>%s</b> (UTC 09:00):", sourceName)
		labels = []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}
		values = []string{"0 9 * * 1", "0 9 * * 2", "0 9 * * 3", "0 9 * * 4", "0 9 * * 5", "0 9 * * 6", "0 9 * * 0"}

	default:
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Unknown condition type.")
		return
	}

	// build one row per button so each fits within the 64-byte callback_data limit
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(values)+1)
	for i, v := range values {
		data := prefix + v
		if len(data) > maxCallbackDataBytes {
			fmt.Printf("telegramapi: skipping value %q for source %q: callback data %d bytes\n",
				v, sourceName, len(data))
			continue
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(labels[i], data),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Back", cbBack),
	))

	_ = h.telegramClient.SendHTMLMessageWithKeyboard(ctx,
		integration.TelegramChatID(chatID),
		prompt,
		tgbotapi.NewInlineKeyboardMarkup(rows...))
}

// saveSubscription persists the subscription, notifies the user, and returns to the main menu.
// On failure it notifies and goes back to the main menu as well.
func (h *TelegramApi) saveSubscription(
	ctx context.Context,
	chatID int64,
	sub *domain.RateUserSubscription,
) {
	if err := h.subRepo.RetainRateUserSubscription(ctx, sub); err != nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Failed to save subscription.")
		h.sendMainMenu(ctx, chatID)
		return
	}
	_ = h.telegramClient.SendHTMLMessage(ctx,
		integration.TelegramChatID(chatID),
		fmt.Sprintf("✅ Subscribed to <b>%s</b>.", sub.SourceName))
	h.sendMainMenu(ctx, chatID)
}

// handleDeleteList fetches the caller's subscriptions and shows each as a delete button.
// Callback data format: sub:del:<urlencoded_source_name>
// No state is stored — the source name travels in the callback data.
func (h *TelegramApi) handleDeleteList(ctx context.Context, chatID int64, _ int) {
	subs, err := h.subRepo.ObtainRateUserSubscriptionsByUserID(
		ctx, domain.UserTypeTelegram, strconv.FormatInt(chatID, 10),
	)
	if err != nil || len(subs) == 0 {
		_ = h.telegramClient.SendHTMLMessageWithKeyboard(ctx,
			integration.TelegramChatID(chatID),
			"You have no subscriptions to delete.", backKeyboard())
		return
	}
	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(subs)+1)
	for _, s := range subs {
		label := fmt.Sprintf("🗑 %s — %s", s.SourceName, subscriptionConditionLabel(s))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label,
				"sub:del:"+url.QueryEscape(s.SourceName)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Back", cbBack),
	))
	_ = h.telegramClient.SendHTMLMessageWithKeyboard(ctx,
		integration.TelegramChatID(chatID),
		"Select a subscription to <b>delete</b>:",
		tgbotapi.NewInlineKeyboardMarkup(rows...))
}

// handleDeleteAsk asks for confirmation before deletion.
// sourceName is already URL-decoded by the caller (handleCallback) — re-encode it
// when building the "yes" button callback data.
func (h *TelegramApi) handleDeleteAsk(ctx context.Context, chatID int64, _ int, sourceName string) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Yes, delete",
				"sub:del:yes:"+url.QueryEscape(sourceName)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", cbDelNo),
		),
	)
	_ = h.telegramClient.SendHTMLMessageWithKeyboard(ctx,
		integration.TelegramChatID(chatID),
		fmt.Sprintf("Delete subscription to <b>%s</b>?", sourceName), kb)
}

// handleDeleteConfirm deletes the subscription, notifies the user, and returns to the main menu.
func (h *TelegramApi) handleDeleteConfirm(ctx context.Context, chatID int64, sourceName string) {
	sub := &domain.RateUserSubscription{
		UserType:   domain.UserTypeTelegram,
		UserID:     strconv.FormatInt(chatID, 10),
		SourceName: sourceName,
	}
	if err := h.subRepo.RemoveRateUserSubscription(ctx, sub); err != nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Failed to delete subscription.")
	} else {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID),
			fmt.Sprintf("🗑 Subscription to <b>%s</b> deleted.", sourceName))
	}
	h.sendMainMenu(ctx, chatID)
}

const (
	cbShow         = "sub:show"
	cbAdd          = "sub:add"
	cbLatest       = "sub:latest"
	cbAddSrcPrefix = "sub:add:" // prefix for stateless add flow: sub:add:<source>[:<ct>[:<value>]]
	cbDelete       = "sub:delete"
	cbBack         = "sub:back"
	cbDelNo        = "sub:del:no"

	commandStart         = "/start"
	commandSubscriptions = "/subscriptions"

	// maxCallbackDataBytes is Telegram's hard limit on callback_data length.
	maxCallbackDataBytes = 64
)

// telegramClient interface — includes all methods needed by the handlers.
// *integration.TelegramBotClient satisfies this interface.
type telegramClient interface {
	Listen(context.Context, integration.UpdateHandler)
	SendPlainTextMessage(context.Context, integration.TelegramChatID, string) error
	SendMarkdownMessage(context.Context, integration.TelegramChatID, string) error
	SendHTMLMessage(context.Context, integration.TelegramChatID, string) error
	SendHTMLMessageWithKeyboard(context.Context, integration.TelegramChatID, string, tgbotapi.InlineKeyboardMarkup) error
	AnswerCallbackQuery(context.Context, string, string) error
}

type subscriptionRepository interface {
	ObtainRateUserSubscriptionsByUserID(ctx context.Context, userType domain.UserType, userID string) ([]domain.RateUserSubscription, error)
	RetainRateUserSubscription(ctx context.Context, sub *domain.RateUserSubscription) error
	RemoveRateUserSubscription(ctx context.Context, sub *domain.RateUserSubscription) error
}

type sourceRepository interface {
	ObtainRateSourceByName(ctx context.Context, name string) (*domain.RateSource, error)
	ObtainAllRateSources(ctx context.Context) ([]domain.RateSource, error)
}

func backKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("« Back", cbBack),
		),
	)
}

// subscriptionConditionLabel returns a human-readable description of a subscription's condition.
func subscriptionConditionLabel(s domain.RateUserSubscription) string {
	switch s.ConditionType {
	case domain.ConditionTypeDelta:
		return fmt.Sprintf("Δ ≥ %s%%", s.ConditionValue)
	case domain.ConditionTypeInterval:
		return fmt.Sprintf("every %s", intervalLabel(s.ConditionValue))
	case domain.ConditionTypeDaily:
		// stored as "15:04:05" — show only HH:MM UTC
		if len(s.ConditionValue) >= 5 {
			return fmt.Sprintf("daily at %s UTC", s.ConditionValue[:5])
		}
		return fmt.Sprintf("daily at %s UTC", s.ConditionValue)
	case domain.ConditionTypeCron:
		return fmt.Sprintf("weekly on %s (UTC 09:00)", cronWeekdayLabel(s.ConditionValue))
	default:
		return string(s.ConditionType)
	}
}

// intervalLabel maps raw duration strings to short human-readable labels.
func intervalLabel(v string) string {
	switch v {
	case "24h":
		return "1d"
	case "168h":
		return "1w"
	default:
		return v
	}
}

// cronWeekdayLabel extracts the weekday name from a "0 9 * * N" cron expression.
func cronWeekdayLabel(expr string) string {
	days := map[string]string{
		"0": "Sunday", "1": "Monday", "2": "Tuesday",
		"3": "Wednesday", "4": "Thursday", "5": "Friday", "6": "Saturday",
	}
	parts := strings.Fields(expr)
	if len(parts) == 5 {
		if name, ok := days[parts[4]]; ok {
			return name
		}
	}
	return expr
}

// conditionFromString maps a callback payload segment to a domain condition type.
// Returns an empty string for unknown input — callers must check for this.
func conditionFromString(s string) domain.SubscriptionConditionType {
	switch s {
	case "delta":
		return domain.ConditionTypeDelta
	case "interval":
		return domain.ConditionTypeInterval
	case "daily":
		return domain.ConditionTypeDaily
	case "cron":
		return domain.ConditionTypeCron
	default:
		return ""
	}
}
