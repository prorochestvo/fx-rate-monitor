// // Package telegrambot implements the Telegram bot interaction layer.
// // SubscriptionHandler drives the full subscription CRUD flow via inline keyboards.
package api

//
//import (
//	"context"
//	"fmt"
//	"log"
//	"net/url"
//	"strconv"
//	"strings"
//	"sync"
//	"time"
//
//	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
//	"github.com/seilbekskindirov/monitor/internal/domain"
//	integration "github.com/seilbekskindirov/monitor/internal/infrastructure/telegrambot"
//)
//
//// ---- narrow dependency interfaces ----
//
//type botClient interface {
//	SendHTMLMessage(ctx context.Context, chatID integration.TelegramChatID, text string) error
//	SendHTMLMessageWithKeyboard(ctx context.Context, chatID integration.TelegramChatID, text string, kb tgbotapi.InlineKeyboardMarkup) error
//	AnswerCallbackQuery(ctx context.Context, callbackQueryID, text string) error
//	EditMessageText(ctx context.Context, chatID integration.TelegramChatID, messageID int, text string) error
//}
//
//type subscriptionRepository interface {
//	ObtainRateUserSubscriptionsByUserID(ctx context.Context, userType domain.UserType, userID string) ([]domain.RateUserSubscription, error)
//	RetainRateUserSubscription(ctx context.Context, sub *domain.RateUserSubscription) error
//	RemoveRateUserSubscription(ctx context.Context, sub *domain.RateUserSubscription) error
//}
//
//type sourceRepository interface {
//	ObtainRateSourceByName(ctx context.Context, name string) (*domain.RateSource, error)
//	ObtainAllRateSources(ctx context.Context) ([]domain.RateSource, error)
//}
//
//// ---- callback data constants ----
//
//const (
//	cbShow        = "sub:show"
//	cbAdd         = "sub:add"
//	cbAddDelta    = "sub:add:delta"
//	cbAddInterval = "sub:add:interval"
//	cbDelete      = "sub:delete"
//	cbUpdate      = "sub:update"
//	cbBack        = "sub:back"
//	cbDelNo       = "sub:del:no"
//	cbUpdNo       = "sub:upd:no"
//
//	idleTimeout = 15 * time.Minute
//)
//
//// ---- session state machine ----
//
//type conversationState struct {
//	Action        string                           // "add" | "delete" | "update"
//	Step          int                              // step index within the flow
//	SourceName    string                           // selected source (mid-flow)
//	ConditionType domain.SubscriptionConditionType // condition type selected at step 2
//	MessageID     int                              // ID of the bot's menu message, for in-place edits
//	LastActive    time.Time
//}
//
//// ---- handler ----
//
//// SubscriptionHandler implements Telegram bot subscription CRUD via inline keyboards.
//// It is stateful per chatID; state is stored in memory with a 15-minute idle timeout.
//type SubscriptionHandler struct {
//	mu         sync.Mutex
//	states     map[int64]*conversationState
//	tbot       botClient
//	subRepo    subscriptionRepository
//	sourceRepo sourceRepository
//}
//
//// NewSubscriptionHandler constructs a handler. All three arguments are required.
//func NewSubscriptionHandler(
//	tbot botClient,
//	subRepo subscriptionRepository,
//	sourceRepo sourceRepository,
//) *SubscriptionHandler {
//	return &SubscriptionHandler{
//		states:     make(map[int64]*conversationState),
//		tbot:       tbot,
//		subRepo:    subRepo,
//		sourceRepo: sourceRepo,
//	}
//}
//
//// Handle is the entry point compatible with integration.UpdateHandler.
//// It dispatches to the appropriate flow based on the update type.
//func (h *SubscriptionHandler) Handle(ctx context.Context, update tgbotapi.Update) {
//	switch {
//	case update.CallbackQuery != nil:
//		h.handleCallback(ctx, update.CallbackQuery)
//	case update.Message != nil:
//		h.handleMessage(ctx, update.Message)
//	}
//}
//
//// ---- message handler ----
//
//func (h *SubscriptionHandler) handleMessage(ctx context.Context, msg *tgbotapi.Message) {
//	chatID := msg.Chat.ID
//	text := strings.TrimSpace(msg.Text)
//
//	// Commands always reset state and show the main menu.
//	if text == "/subscriptions" || text == "/start" {
//		h.resetState(chatID)
//		h.sendMainMenu(ctx, chatID)
//		return
//	}
//
//	state := h.getState(chatID)
//	if state == nil || time.Since(state.LastActive) > idleTimeout {
//		h.resetState(chatID)
//		_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//			"Please use /subscriptions to start over.")
//		return
//	}
//
//	h.touchState(chatID)
//
//	switch state.Action {
//	case "add":
//		h.handleAddText(ctx, chatID, text, state)
//	case "update":
//		h.handleUpdateText(ctx, chatID, text, state)
//	default:
//		_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//			"Please use /subscriptions to start over.")
//	}
//}
//
//// ---- callback handler ----
//
//func (h *SubscriptionHandler) handleCallback(ctx context.Context, cb *tgbotapi.CallbackQuery) {
//	chatID := cb.Message.Chat.ID
//	msgID := cb.Message.MessageID
//	data := cb.Data
//
//	// Always acknowledge to clear the spinner.
//	_ = h.tbot.AnswerCallbackQuery(ctx, cb.ID, "")
//
//	h.touchState(chatID)
//
//	switch {
//	case data == cbBack:
//		h.resetState(chatID)
//		h.sendMainMenu(ctx, chatID)
//
//	case data == cbShow:
//		h.handleShow(ctx, chatID, msgID)
//
//	case data == cbAdd:
//		h.setState(chatID, &conversationState{Action: "add", Step: 1, MessageID: msgID, LastActive: time.Now()})
//		_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//			"Please enter the <b>source name</b> you want to subscribe to:")
//
//	case data == cbAddDelta:
//		h.handleAddCondition(ctx, chatID, domain.ConditionTypeDelta)
//
//	case data == cbAddInterval:
//		h.handleAddCondition(ctx, chatID, domain.ConditionTypeInterval)
//
//	case data == cbDelete:
//		h.handleDeleteList(ctx, chatID, msgID)
//
//	case strings.HasPrefix(data, "sub:del:yes:"):
//		sourceName, _ := url.QueryUnescape(strings.TrimPrefix(data, "sub:del:yes:"))
//		h.handleDeleteConfirm(ctx, chatID, sourceName)
//
//	case strings.HasPrefix(data, "sub:del:"):
//		sourceName, _ := url.QueryUnescape(strings.TrimPrefix(data, "sub:del:"))
//		h.handleDeleteAsk(ctx, chatID, msgID, sourceName)
//
//	case data == cbDelNo:
//		h.resetState(chatID)
//		h.sendMainMenu(ctx, chatID)
//
//	case data == cbUpdate:
//		h.handleUpdateList(ctx, chatID, msgID)
//
//	case strings.HasPrefix(data, "sub:upd:") && !strings.HasPrefix(data, "sub:upd:no") && !strings.Contains(strings.TrimPrefix(data, "sub:upd:"), ":"):
//		sourceName, _ := url.QueryUnescape(strings.TrimPrefix(data, "sub:upd:"))
//		h.handleUpdateSelect(ctx, chatID, msgID, sourceName)
//
//	case strings.HasPrefix(data, "sub:upd:") && strings.Contains(strings.TrimPrefix(data, "sub:upd:"), ":"):
//		// "sub:upd:<source>:<condition>"
//		rest := strings.TrimPrefix(data, "sub:upd:")
//		idx := strings.LastIndex(rest, ":")
//		if idx < 0 {
//			return
//		}
//		sourceName, _ := url.QueryUnescape(rest[:idx])
//		condition := rest[idx+1:]
//		h.handleUpdateCondition(ctx, chatID, sourceName, condition)
//
//	case data == cbUpdNo:
//		h.resetState(chatID)
//		h.sendMainMenu(ctx, chatID)
//	}
//}
//
//// ---- main menu ----
//
//func (h *SubscriptionHandler) sendMainMenu(ctx context.Context, chatID int64) {
//	kb := tgbotapi.NewInlineKeyboardMarkup(
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("📋 Show subscriptions", cbShow),
//		),
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("➕ Add subscription", cbAdd),
//			tgbotapi.NewInlineKeyboardButtonData("✏️ Update subscription", cbUpdate),
//		),
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("🗑 Delete subscription", cbDelete),
//		),
//	)
//	_ = h.tbot.SendHTMLMessageWithKeyboard(ctx, integration.TelegramChatID(chatID),
//		"<b>Subscription Management</b>\nChoose an action:", kb)
//}
//
//// ---- show flow ----
//
//func (h *SubscriptionHandler) handleShow(ctx context.Context, chatID int64, msgID int) {
//	subs, err := h.subRepo.ObtainRateUserSubscriptionsByUserID(ctx, domain.UserTypeTelegram, strconv.FormatInt(chatID, 10))
//	if err != nil {
//		log.Printf("subscriptionhandler: show: %v", err)
//		_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID), "⚠️ Failed to load subscriptions.")
//		return
//	}
//
//	if len(subs) == 0 {
//		kb := backKeyboard()
//		_ = h.tbot.SendHTMLMessageWithKeyboard(ctx, integration.TelegramChatID(chatID),
//			"You have no active subscriptions.", kb)
//		return
//	}
//
//	var sb strings.Builder
//	sb.WriteString("<b>Your subscriptions:</b>\n")
//	for _, s := range subs {
//		sb.WriteString(fmt.Sprintf(" • <b>%s</b> — %s", s.SourceName, subscriptionConditionLabel(s)))
//		if s.ConditionType == domain.ConditionTypeDelta && s.DeltaThreshold > 0 {
//			sb.WriteString(fmt.Sprintf(" (≥ %.4f)", s.DeltaThreshold))
//		}
//		sb.WriteString("\n")
//	}
//
//	kb := backKeyboard()
//	_ = h.tbot.SendHTMLMessageWithKeyboard(ctx, integration.TelegramChatID(chatID), sb.String(), kb)
//	_ = msgID // suppress unused warning; EditMessageText is optional here
//}
//
//// ---- add flow ----
//
//// handleAddText handles text input for the Add flow.
////
//// Step mapping:
////
////	Step 1 — source name
////	Step 2 — condition type (keyboard; handled via callback)
////	Step 3 — delta threshold (float) OR interval duration string, depending on state.ConditionType
//func (h *SubscriptionHandler) handleAddText(ctx context.Context, chatID int64, text string, state *conversationState) {
//	switch state.Step {
//	case 1: // expecting source name
//		source, err := h.sourceRepo.ObtainRateSourceByName(ctx, text)
//		if err != nil || source == nil {
//			_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//				fmt.Sprintf("⚠️ SourceName <b>%s</b> not found. Please try again:", text))
//			return
//		}
//		state.SourceName = source.Name
//		state.Step = 2
//		h.touchState(chatID)
//		h.sendConditionTypeKeyboard(ctx, chatID)
//
//	case 3:
//		switch state.ConditionType {
//		case domain.ConditionTypeDelta:
//			threshold, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", "."), 64)
//			if err != nil || threshold < 0 {
//				_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//					"⚠️ Invalid threshold. Please enter a positive number (e.g. <code>0.5</code>):")
//				return
//			}
//			h.saveSubscription(ctx, chatID, state.SourceName, domain.ConditionTypeDelta, threshold, 0)
//			h.resetState(chatID)
//
//		case domain.ConditionTypeInterval:
//			d, err := time.ParseDuration(text)
//			if err != nil || d < time.Minute {
//				_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//					"⚠️ Invalid interval. Use Go duration format (e.g. <code>30m</code>, <code>6h</code>). Minimum is <code>1m</code>:")
//				return
//			}
//			h.saveSubscription(ctx, chatID, state.SourceName, domain.ConditionTypeInterval, 0, d)
//			h.resetState(chatID)
//		}
//	}
//}
//
//func (h *SubscriptionHandler) sendConditionTypeKeyboard(ctx context.Context, chatID int64) {
//	kb := tgbotapi.NewInlineKeyboardMarkup(
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("📉 Delta", cbAddDelta),
//		),
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("⏰ Custom interval", cbAddInterval),
//		),
//	)
//	_ = h.tbot.SendHTMLMessageWithKeyboard(ctx, integration.TelegramChatID(chatID),
//		"Choose the <b>notification condition</b>:", kb)
//}
//
//func (h *SubscriptionHandler) handleAddCondition(ctx context.Context, chatID int64, ct domain.SubscriptionConditionType) {
//	state := h.getState(chatID)
//	if state == nil || state.Action != "add" {
//		h.resetState(chatID)
//		h.sendMainMenu(ctx, chatID)
//		return
//	}
//
//	state.ConditionType = ct
//
//	switch ct {
//	case domain.ConditionTypeDelta:
//		state.Step = 3
//		h.touchState(chatID)
//		_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//			"Enter the <b>minimum delta threshold</b> (e.g. <code>0.5</code>), or <code>0</code> for any change:")
//	case domain.ConditionTypeInterval:
//		state.Step = 3
//		h.touchState(chatID)
//		_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//			"Enter the <b>notification interval</b> (e.g. <code>30m</code>, <code>6h</code>, <code>24h</code>, <code>1h30m</code>).\nMinimum: <code>1m</code>.")
//	default:
//		h.saveSubscription(ctx, chatID, state.SourceName, ct, 0, 0)
//		h.resetState(chatID)
//	}
//}
//
//func (h *SubscriptionHandler) saveSubscription(
//	ctx context.Context, chatID int64,
//	sourceName string,
//	ct domain.SubscriptionConditionType,
//	threshold float64,
//	notifyInterval time.Duration,
//) {
//	sub := &domain.RateUserSubscription{
//		UserType:       domain.UserTypeTelegram,
//		UserID:         strconv.FormatInt(chatID, 10),
//		SourceName:     sourceName,
//		ConditionType:  ct,
//		DeltaThreshold: threshold,
//		NotifyInterval: notifyInterval,
//	}
//	if err := h.subRepo.RetainRateUserSubscription(ctx, sub); err != nil {
//		log.Printf("subscriptionhandler: save subscription: %v", err)
//		_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID), "⚠️ Failed to save subscription.")
//		return
//	}
//	_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//		fmt.Sprintf("✅ Subscribed to <b>%s</b>.", sourceName))
//}
//
//// ---- delete flow ----
//
//func (h *SubscriptionHandler) handleDeleteList(ctx context.Context, chatID int64, msgID int) {
//	subs, err := h.subRepo.ObtainRateUserSubscriptionsByUserID(ctx, domain.UserTypeTelegram, strconv.FormatInt(chatID, 10))
//	if err != nil || len(subs) == 0 {
//		kb := backKeyboard()
//		_ = h.tbot.SendHTMLMessageWithKeyboard(ctx, integration.TelegramChatID(chatID),
//			"You have no subscriptions to delete.", kb)
//		return
//	}
//
//	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(subs)+1)
//	for _, s := range subs {
//		label := fmt.Sprintf("%s — %s", s.SourceName, subscriptionConditionLabel(s))
//		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData(label, "sub:del:"+url.QueryEscape(s.SourceName)),
//		))
//	}
//	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
//		tgbotapi.NewInlineKeyboardButtonData("« Back", cbBack),
//	))
//	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
//	_ = h.tbot.SendHTMLMessageWithKeyboard(ctx, integration.TelegramChatID(chatID),
//		"Select a subscription to <b>delete</b>:", kb)
//	_ = msgID
//}
//
//func (h *SubscriptionHandler) handleDeleteAsk(ctx context.Context, chatID int64, msgID int, sourceName string) {
//	h.setState(chatID, &conversationState{Action: "delete", SourceName: sourceName, MessageID: msgID, LastActive: time.Now()})
//
//	kb := tgbotapi.NewInlineKeyboardMarkup(
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("✅ Confirm", "sub:del:yes:"+url.QueryEscape(sourceName)),
//			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", cbDelNo),
//		),
//	)
//	_ = h.tbot.SendHTMLMessageWithKeyboard(ctx, integration.TelegramChatID(chatID),
//		fmt.Sprintf("Delete subscription to <b>%s</b>?", sourceName), kb)
//}
//
//func (h *SubscriptionHandler) handleDeleteConfirm(ctx context.Context, chatID int64, sourceName string) {
//	sub := &domain.RateUserSubscription{
//		UserType:   domain.UserTypeTelegram,
//		UserID:     strconv.FormatInt(chatID, 10),
//		SourceName: sourceName,
//	}
//	if err := h.subRepo.RemoveRateUserSubscription(ctx, sub); err != nil {
//		log.Printf("subscriptionhandler: delete: %v", err)
//		_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID), "⚠️ Failed to delete subscription.")
//	} else {
//		_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//			fmt.Sprintf("🗑 Subscription to <b>%s</b> deleted.", sourceName))
//	}
//	h.resetState(chatID)
//}
//
//// ---- update flow ----
//
//func (h *SubscriptionHandler) handleUpdateList(ctx context.Context, chatID int64, msgID int) {
//	subs, err := h.subRepo.ObtainRateUserSubscriptionsByUserID(ctx, domain.UserTypeTelegram, strconv.FormatInt(chatID, 10))
//	if err != nil || len(subs) == 0 {
//		kb := backKeyboard()
//		_ = h.tbot.SendHTMLMessageWithKeyboard(ctx, integration.TelegramChatID(chatID),
//			"You have no subscriptions to update.", kb)
//		return
//	}
//
//	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(subs)+1)
//	for _, s := range subs {
//		label := fmt.Sprintf("%s — %s", s.SourceName, subscriptionConditionLabel(s))
//		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData(label, "sub:upd:"+url.QueryEscape(s.SourceName)),
//		))
//	}
//	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
//		tgbotapi.NewInlineKeyboardButtonData("« Back", cbBack),
//	))
//	kb := tgbotapi.NewInlineKeyboardMarkup(rows...)
//	_ = h.tbot.SendHTMLMessageWithKeyboard(ctx, integration.TelegramChatID(chatID),
//		"Select a subscription to <b>update</b>:", kb)
//	_ = msgID
//}
//
//func (h *SubscriptionHandler) handleUpdateSelect(ctx context.Context, chatID int64, msgID int, sourceName string) {
//	h.setState(chatID, &conversationState{Action: "update", Step: 1, SourceName: sourceName, MessageID: msgID, LastActive: time.Now()})
//
//	kb := tgbotapi.NewInlineKeyboardMarkup(
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("📉 Delta", "sub:upd:"+url.QueryEscape(sourceName)+":delta"),
//		),
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("⏰ Custom interval", "sub:upd:"+url.QueryEscape(sourceName)+":interval"),
//		),
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("❌ Cancel", cbUpdNo),
//		),
//	)
//	_ = h.tbot.SendHTMLMessageWithKeyboard(ctx, integration.TelegramChatID(chatID),
//		fmt.Sprintf("Choose new condition for <b>%s</b>:", sourceName), kb)
//}
//
//func (h *SubscriptionHandler) handleUpdateCondition(ctx context.Context, chatID int64, sourceName, condition string) {
//	ct := conditionFromString(condition)
//	if ct == "" {
//		_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID), "⚠️ Unknown condition type.")
//		h.resetState(chatID)
//		return
//	}
//
//	switch ct {
//	case domain.ConditionTypeDelta:
//		state := h.getState(chatID)
//		if state != nil {
//			state.Step = 2
//			state.SourceName = sourceName
//			state.ConditionType = domain.ConditionTypeDelta
//			h.touchState(chatID)
//		}
//		_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//			"Enter the <b>minimum delta threshold</b> (e.g. <code>0.5</code>), or <code>0</code> for any change:")
//
//	case domain.ConditionTypeInterval:
//		state := h.getState(chatID)
//		if state != nil {
//			state.Step = 2
//			state.SourceName = sourceName
//			state.ConditionType = domain.ConditionTypeInterval
//			h.touchState(chatID)
//		}
//		_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//			"Enter the <b>notification interval</b> (e.g. <code>30m</code>, <code>6h</code>, <code>24h</code>).\nMinimum: <code>1m</code>.")
//
//	default:
//		h.saveSubscription(ctx, chatID, sourceName, ct, 0, 0)
//		h.resetState(chatID)
//	}
//}
//
//func (h *SubscriptionHandler) handleUpdateText(ctx context.Context, chatID int64, text string, state *conversationState) {
//	if state.Step != 2 {
//		return
//	}
//	switch state.ConditionType {
//	case domain.ConditionTypeDelta:
//		threshold, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", "."), 64)
//		if err != nil || threshold < 0 {
//			_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//				"⚠️ Invalid threshold. Please enter a positive number (e.g. <code>0.5</code>):")
//			return
//		}
//		h.saveSubscription(ctx, chatID, state.SourceName, domain.ConditionTypeDelta, threshold, 0)
//		h.resetState(chatID)
//
//	case domain.ConditionTypeInterval:
//		d, err := time.ParseDuration(text)
//		if err != nil || d < time.Minute {
//			_ = h.tbot.SendHTMLMessage(ctx, integration.TelegramChatID(chatID),
//				"⚠️ Invalid interval. Use Go duration format (e.g. <code>30m</code>, <code>6h</code>). Minimum is <code>1m</code>:")
//			return
//		}
//		h.saveSubscription(ctx, chatID, state.SourceName, domain.ConditionTypeInterval, 0, d)
//		h.resetState(chatID)
//
//	default:
//		h.resetState(chatID)
//		h.sendMainMenu(ctx, chatID)
//	}
//}
//
//// ---- state helpers ----
//
//func (h *SubscriptionHandler) getState(chatID int64) *conversationState {
//	h.mu.Lock()
//	defer h.mu.Unlock()
//	return h.states[chatID]
//}
//
//func (h *SubscriptionHandler) setState(chatID int64, state *conversationState) {
//	h.mu.Lock()
//	defer h.mu.Unlock()
//	h.states[chatID] = state
//}
//
//func (h *SubscriptionHandler) resetState(chatID int64) {
//	h.mu.Lock()
//	defer h.mu.Unlock()
//	delete(h.states, chatID)
//}
//
//func (h *SubscriptionHandler) touchState(chatID int64) {
//	h.mu.Lock()
//	defer h.mu.Unlock()
//	if s, ok := h.states[chatID]; ok {
//		s.LastActive = time.Now()
//	}
//}
//
//// ---- utility ----
//
//func backKeyboard() tgbotapi.InlineKeyboardMarkup {
//	return tgbotapi.NewInlineKeyboardMarkup(
//		tgbotapi.NewInlineKeyboardRow(
//			tgbotapi.NewInlineKeyboardButtonData("« Back", cbBack),
//		),
//	)
//}
//
//// subscriptionConditionLabel returns a human-readable label for a subscription's condition.
//// For ConditionTypeInterval it includes the configured duration.
//func subscriptionConditionLabel(s domain.RateUserSubscription) string {
//	switch s.ConditionType {
//	case domain.ConditionTypeDelta:
//		return "delta"
//	case domain.ConditionTypeInterval:
//		return fmt.Sprintf("every %s", formatDuration(s.NotifyInterval))
//	case domain.ConditionTypeDailyAt8AM:
//		return "daily 8AM"
//	case domain.ConditionTypeEvery6Hours:
//		return "every 6h"
//	default:
//		return string(s.ConditionType)
//	}
//}
//
//// formatDuration trims a trailing "0s" suffix so "30m0s" becomes "30m".
//func formatDuration(d time.Duration) string {
//	s := d.String()
//	if strings.HasSuffix(s, "m0s") {
//		s = strings.TrimSuffix(s, "0s")
//	}
//	return s
//}
//
//// conditionFromString maps a callback payload segment to a domain condition type.
//// Legacy values "daily8am" and "every6h" are kept for in-flight Telegram sessions.
//func conditionFromString(s string) domain.SubscriptionConditionType {
//	switch s {
//	case "delta":
//		return domain.ConditionTypeDelta
//	case "interval":
//		return domain.ConditionTypeInterval
//	case "daily8am":
//		return domain.ConditionTypeDailyAt8AM
//	case "every6h":
//		return domain.ConditionTypeEvery6Hours
//	default:
//		return ""
//	}
//}
