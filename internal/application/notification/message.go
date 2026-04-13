package notification

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

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
		if alertItem.Delta != 0 {
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
		sources = append(sources, fmt.Sprintf("%s:\n%s\n", title, strings.Join(values, "\n")))
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
