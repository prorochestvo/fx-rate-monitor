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
		h.sendMainMenu(ctx, chatID, 0)
		return
	}

	_ = h.telegramClient.SendHTMLMessage(ctx,
		integration.TelegramChatID(chatID),
		fmt.Sprintf("Please use %s to start.", commandSubscriptions))
}

// handleCallback routes inline-keyboard presses to the correct handler.
//
// Add-flow callback_data layout (all segments are URL-encoded where noted):
//
//	sub:add                          — show unique source titles (step A)
//	sub:add:title:<title>            — title chosen → show currency pairs (step B)
//	sub:add:<src>                    — pair chosen  → show condition types (step C)
//	sub:add:<src>:delta              — delta chosen  → show delta value buttons
//	sub:add:<src>:interval           — interval chosen → show interval value buttons
//	sub:add:<src>:delta:<val>        — value chosen  → save subscription
//	sub:add:<src>:interval:<val>     — value chosen  → save subscription
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
		h.sendMainMenu(ctx, chatID, msgID)

	case data == cbShow:
		h.handleShow(ctx, chatID, msgID)

	case data == cbLatest:
		h.handleLatestUpdates(ctx, chatID, msgID)

	case data == cbAdd:
		h.handleAddSourceList(ctx, chatID, msgID)

	case strings.HasPrefix(data, cbAddTitlePrefix):
		title, _ := url.QueryUnescape(strings.TrimPrefix(data, cbAddTitlePrefix))
		h.handleAddTitleSelect(ctx, chatID, msgID, title)

	case strings.HasPrefix(data, cbAddSrcPrefix):
		h.routeAddFlow(ctx, chatID, msgID, strings.TrimPrefix(data, cbAddSrcPrefix))

	case data == cbDelete:
		h.handleDeleteList(ctx, chatID, msgID)

	// sub:del:yes: must be checked before sub:del: — the latter is a prefix of the former
	case strings.HasPrefix(data, "sub:del:yes:"):
		sourceName, _ := url.QueryUnescape(strings.TrimPrefix(data, "sub:del:yes:"))
		h.handleDeleteConfirm(ctx, chatID, msgID, sourceName)

	case strings.HasPrefix(data, "sub:del:"):
		sourceName, _ := url.QueryUnescape(strings.TrimPrefix(data, "sub:del:"))
		h.handleDeleteAsk(ctx, chatID, msgID, sourceName)

	case data == cbDelNo:
		h.sendMainMenu(ctx, chatID, msgID)
	}
}

// routeAddFlow dispatches the add-subscription flow based on the number of
// decoded segments after "sub:add:".
//
//	1 segment  → source chosen
//	2 segments → condition type chosen
//	3 segments → value chosen → save
func (h *TelegramApi) routeAddFlow(ctx context.Context, chatID int64, msgID int, rest string) {
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
		h.handleAddSourceSelect(ctx, chatID, msgID, sourceName)

	case 2:
		// condition type selected → show value buttons
		ct := conditionFromString(parts[1])
		if ct == "" {
			return
		}
		h.handleAddValueSelect(ctx, chatID, msgID, sourceName, ct)

	case 3:
		// value selected → save subscription
		ct := conditionFromString(parts[1])
		value := parts[2]
		if ct == "" || value == "" {
			return
		}
		h.saveSubscription(ctx, chatID, msgID, &domain.RateUserSubscription{
			UserType:       domain.UserTypeTelegram,
			UserID:         strconv.FormatInt(chatID, 10),
			SourceName:     sourceName,
			ConditionType:  ct,
			ConditionValue: value,
		})
	}
}

// sendMainMenu shows the top-level keyboard. When msgID > 0 the existing message is edited
// in place (callback flow); when 0 a new message is sent (text-command flow).
func (h *TelegramApi) sendMainMenu(ctx context.Context, chatID int64, msgID int) {
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
	const text = "<b>Subscription Management</b>\nChoose an action:"
	h.sendOrEditWithKeyboard(ctx, chatID, msgID, text, kb)
}

// handleShow lists the caller's active subscriptions.
func (h *TelegramApi) handleShow(ctx context.Context, chatID int64, msgID int) {
	subs, err := h.subRepo.ObtainRateUserSubscriptionsByUserID(
		ctx, domain.UserTypeTelegram, strconv.FormatInt(chatID, 10),
	)
	if err != nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Failed to load subscriptions.")
		return
	}
	if len(subs) == 0 {
		h.sendOrEditWithKeyboard(ctx, chatID, msgID, "You have no active subscriptions.", backKeyboard())
		return
	}
	var sb strings.Builder
	sb.WriteString("<b>Your subscriptions:</b>\n")
	for _, s := range subs {
		sb.WriteString(fmt.Sprintf(" • <b>%s</b> — %s\n",
			s.SourceName, subscriptionConditionLabel(s)))
	}
	h.sendOrEditWithKeyboard(ctx, chatID, msgID, sb.String(), backKeyboard())
}

// handleLatestUpdates shows the last known rate for each of the caller's subscriptions.
// It displays the rate at the time of the last notification (LatestNotifiedRate + UpdatedAt),
// not the live market rate — this is intentional for MVP.
func (h *TelegramApi) handleLatestUpdates(ctx context.Context, chatID int64, msgID int) {
	subs, err := h.subRepo.ObtainRateUserSubscriptionsByUserID(
		ctx, domain.UserTypeTelegram, strconv.FormatInt(chatID, 10),
	)
	if err != nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Failed to load subscriptions.")
		return
	}
	if len(subs) == 0 {
		h.sendOrEditWithKeyboard(ctx, chatID, msgID, "You have no subscriptions yet.", backKeyboard())
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
	h.sendOrEditWithKeyboard(ctx, chatID, msgID, sb.String(), backKeyboard())
}

// handleAddSourceList fetches all rate sources, deduplicates them by title (preserving
// insertion order), and presents one button per unique title as an inline keyboard.
// Callback data format: sub:add:title:<urlencoded_title>
//
// NOTE: Telegram enforces a hard 64-byte limit on callback_data. Titles longer than
// ~49 characters may exceed this limit after URL-encoding. Buttons with overlong callback
// data are skipped and a warning is printed to stderr.
func (h *TelegramApi) handleAddSourceList(ctx context.Context, chatID int64, msgID int) {
	sources, err := h.sourceRepo.ObtainAllRateSources(ctx)
	if err != nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Failed to load rate sources.")
		return
	}
	if len(sources) == 0 {
		h.sendOrEditWithKeyboard(ctx, chatID, msgID, "No rate sources are configured yet.", backKeyboard())
		return
	}
	seen := make(map[string]struct{})
	rows := make([][]tgbotapi.InlineKeyboardButton, 0)
	for _, src := range sources {
		if _, ok := seen[src.Title]; ok {
			continue
		}
		seen[src.Title] = struct{}{}
		data := cbAddTitlePrefix + url.QueryEscape(src.Title)
		if len(data) > maxCallbackDataBytes {
			fmt.Printf("telegramapi: skipping title %q: callback_data %d bytes exceeds 64-byte limit\n",
				src.Title, len(data))
			continue
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(src.Title, data),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Back", cbBack),
	))
	h.sendOrEditWithKeyboard(ctx, chatID, msgID,
		"Choose a <b>source</b> to subscribe to:",
		tgbotapi.NewInlineKeyboardMarkup(rows...))
}

// handleAddTitleSelect is called when the user picks a source title (step A→B).
// It fetches all rate sources, filters to those matching the chosen title, and presents
// their currency pairs as buttons that feed into the existing routeAddFlow.
// The "Back" button re-fires cbAdd so the user returns to the title list.
func (h *TelegramApi) handleAddTitleSelect(ctx context.Context, chatID int64, msgID int, title string) {
	sources, err := h.sourceRepo.ObtainAllRateSources(ctx)
	if err != nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Failed to load rate sources.")
		return
	}
	rows := make([][]tgbotapi.InlineKeyboardButton, 0)
	for _, src := range sources {
		if src.Title != title {
			continue
		}
		label := src.BaseCurrency + "/" + src.QuoteCurrency
		data := cbAddSrcPrefix + url.QueryEscape(src.Name)
		if len(data) > maxCallbackDataBytes {
			fmt.Printf("telegramapi: skipping pair %q for title %q: callback_data %d bytes\n",
				label, title, len(data))
			continue
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, data),
		))
	}
	if len(rows) == 0 {
		h.sendOrEditWithKeyboard(ctx, chatID, msgID,
			"No currency pairs found for this source.",
			tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(
					tgbotapi.NewInlineKeyboardButtonData("« Back", cbAdd),
				),
			))
		return
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("« Back", cbAdd),
	))
	h.sendOrEditWithKeyboard(ctx, chatID, msgID,
		fmt.Sprintf("Choose a <b>currency pair</b> for <b>%s</b>:", title),
		tgbotapi.NewInlineKeyboardMarkup(rows...))
}

// handleAddSourceSelect shows the condition type buttons after a source is chosen.
// Callback data format: sub:add:<urlencoded_source>:delta or :interval
func (h *TelegramApi) handleAddSourceSelect(ctx context.Context, chatID int64, msgID int, sourceName string) {
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
	h.sendOrEditWithKeyboard(ctx, chatID, msgID,
		fmt.Sprintf("Choose notification condition for <b>%s</b>:", sourceName), kb)
}

// handleAddValueSelect presents preset value buttons for the chosen condition type.
// Callback data format: sub:add:<urlencoded_source>:<conditionType>:<value>
func (h *TelegramApi) handleAddValueSelect(ctx context.Context, chatID int64, msgID int, sourceName string, ct domain.SubscriptionConditionType) {
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

	h.sendOrEditWithKeyboard(ctx, chatID, msgID, prompt, tgbotapi.NewInlineKeyboardMarkup(rows...))
}

// saveSubscription persists the subscription, notifies the user, and returns to the main menu.
// On failure it notifies and goes back to the main menu as well.
func (h *TelegramApi) saveSubscription(ctx context.Context, chatID int64, msgID int, sub *domain.RateUserSubscription) {
	if err := h.subRepo.RetainRateUserSubscription(ctx, sub); err != nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Failed to save subscription.")
		h.sendMainMenu(ctx, chatID, msgID)
		return
	}
	_ = h.telegramClient.SendHTMLMessage(ctx,
		integration.TelegramChatID(chatID),
		fmt.Sprintf("✅ Subscribed to <b>%s</b>.", sub.SourceName))
	h.sendMainMenu(ctx, chatID, msgID)
}

// handleDeleteList fetches the caller's subscriptions and shows each as a delete button.
// Callback data format: sub:del:<urlencoded_source_name>
// No state is stored — the source name travels in the callback data.
func (h *TelegramApi) handleDeleteList(ctx context.Context, chatID int64, msgID int) {
	subs, err := h.subRepo.ObtainRateUserSubscriptionsByUserID(
		ctx, domain.UserTypeTelegram, strconv.FormatInt(chatID, 10),
	)
	if err != nil || len(subs) == 0 {
		h.sendOrEditWithKeyboard(ctx, chatID, msgID, "You have no subscriptions to delete.", backKeyboard())
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
	h.sendOrEditWithKeyboard(ctx, chatID, msgID,
		"Select a subscription to <b>delete</b>:",
		tgbotapi.NewInlineKeyboardMarkup(rows...))
}

// handleDeleteAsk asks for confirmation before deletion.
// sourceName is already URL-decoded by the caller (handleCallback) — re-encode it
// when building the "yes" button callback data.
func (h *TelegramApi) handleDeleteAsk(ctx context.Context, chatID int64, msgID int, sourceName string) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Yes, delete",
				"sub:del:yes:"+url.QueryEscape(sourceName)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", cbDelNo),
		),
	)
	h.sendOrEditWithKeyboard(ctx, chatID, msgID,
		fmt.Sprintf("Delete subscription to <b>%s</b>?", sourceName), kb)
}

// handleDeleteConfirm looks up the subscription by source name, deletes it by its real ID,
// notifies the user, and returns to the main menu.
//
// Fix: previously this function passed an empty ID to RemoveRateUserSubscription, making
// every delete a silent no-op (DELETE WHERE id = ” matches nothing). The fix fetches the
// full subscription list first, finds the matching record, and passes its real ID.
func (h *TelegramApi) handleDeleteConfirm(ctx context.Context, chatID int64, msgID int, sourceName string) {
	userID := strconv.FormatInt(chatID, 10)

	subs, err := h.subRepo.ObtainRateUserSubscriptionsByUserID(ctx, domain.UserTypeTelegram, userID)
	if err != nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Failed to delete subscription.")
		h.sendMainMenu(ctx, chatID, msgID)
		return
	}

	var target *domain.RateUserSubscription
	for i := range subs {
		if subs[i].SourceName == sourceName {
			target = &subs[i]
			break
		}
	}
	if target == nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Subscription not found.")
		h.sendMainMenu(ctx, chatID, msgID)
		return
	}

	if err := h.subRepo.RemoveRateUserSubscription(ctx, target); err != nil {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID), "⚠️ Failed to delete subscription.")
	} else {
		_ = h.telegramClient.SendHTMLMessage(ctx,
			integration.TelegramChatID(chatID),
			fmt.Sprintf("🗑 Subscription to <b>%s</b> deleted.", sourceName))
	}
	h.sendMainMenu(ctx, chatID, msgID)
}

// sendOrEditWithKeyboard sends a new message with keyboard when msgID is zero (text-command
// flow) or edits the existing message in place when msgID > 0 (callback flow). This keeps
// the chat clean by avoiding new chat bubbles on every inline button press.
func (h *TelegramApi) sendOrEditWithKeyboard(ctx context.Context, chatID int64, msgID int, text string, kb tgbotapi.InlineKeyboardMarkup) {
	if msgID > 0 {
		_ = h.telegramClient.EditHTMLMessageWithKeyboard(
			ctx, integration.TelegramChatID(chatID), msgID, text, kb)
		return
	}
	_ = h.telegramClient.SendHTMLMessageWithKeyboard(
		ctx, integration.TelegramChatID(chatID), text, kb)
}

const (
	cbShow           = "sub:show"
	cbAdd            = "sub:add"
	cbLatest         = "sub:latest"
	cbAddSrcPrefix   = "sub:add:"       // prefix for stateless add flow: sub:add:<source>[:<ct>[:<value>]]
	cbAddTitlePrefix = "sub:add:title:" // step A→B: title chosen, show currency pairs
	cbDelete         = "sub:delete"
	cbBack           = "sub:back"
	cbDelNo          = "sub:del:no"

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
	EditHTMLMessageWithKeyboard(context.Context, integration.TelegramChatID, int, string, tgbotapi.InlineKeyboardMarkup) error
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
