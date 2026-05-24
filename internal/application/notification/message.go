package notification

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/tools/labelfmt"
)

const (
	telegramMaxMessageLen = 2048

	// badgeIconDelta is the badge glyph for delta-triggered alerts.
	badgeIconDelta = "Δ" // U+0394

	// badgeIconSchedule is the badge glyph for any time-based condition
	// (interval, daily, cron) that fired in a message part.
	badgeIconSchedule = "⏰" // U+23F0

	// arrowUp is the in-table arrow for a positive delta. ASCII-range compatible
	// with <pre> rendering; replaces the old wide emoji.
	arrowUp = "↑" // U+2191

	// arrowDown is the in-table arrow for a negative delta.
	arrowDown = "↓" // U+2193

	// minusSign is the U+2212 MINUS SIGN used in the delta and value columns so
	// it lines up visually with the ASCII '+' used for positive values.
	minusSign = "−" // U+2212
)

// alert carries the data for one row in the notification table.
// BaseCurrency and QuoteCurrency are passed through html.EscapeString in pairLabel
// before insertion into the HTML <pre> block, so free-text or odd source codes
// cannot break or inject HTML.
type alert struct {
	SourceName    string
	BaseCurrency  string                // e.g. "USD"
	QuoteCurrency string                // e.g. "KZT"
	CurrencyKind  domain.RateSourceKind // BID or ASK
	CurrentPrice  float64               // newest price, e.g. 470.46
	Delta         float64               // signed delta: positive = up, negative = down
	Triggers      []alertTrigger        // ordered: delta, interval, daily, cron
}

// alertTrigger records which condition type (and its collapsed value) caused a
// notification to fire.
type alertTrigger struct {
	ConditionType  domain.SubscriptionConditionType
	ConditionValue string
}

// buildAlertMessage renders alerts into one or more Telegram HTML message parts.
// now is the run timestamp, used verbatim in the header — the function never
// reads time.Now() itself (project preference: clock is injected, not read).
// Returns an empty slice when alerts is empty.
func buildAlertMessage(now time.Time, alerts ...alert) ([]string, error) {
	if len(alerts) == 0 {
		return nil, nil
	}

	sort.Slice(alerts, func(i, j int) bool {
		pi := pairLabel(alerts[i])
		pj := pairLabel(alerts[j])
		return pi < pj
	})

	rows := buildRows(alerts)
	return splitIntoParts(now, rows, alerts), nil
}

// pairLabel returns the display pair string for a row (BID → base/quote, ASK → quote/base).
// Each currency code is HTML-escaped so that future free-text or odd source codes cannot
// break or inject HTML into the <pre> block. Current ASCII codes are unaffected.
func pairLabel(a alert) string {
	base := html.EscapeString(a.BaseCurrency)
	quote := html.EscapeString(a.QuoteCurrency)
	if a.CurrencyKind == domain.RateSourceKindBID {
		return fmt.Sprintf("%s/%s", base, quote)
	}
	return fmt.Sprintf("%s/%s", quote, base)
}

// tableRow holds pre-rendered column strings for one alert row.
type tableRow struct {
	pair  string // e.g. "USD/KZT" or "KZT/USD"
	value string // e.g. "68 382.56"
	delta string // e.g. "+2.60" or "−74.79", or "" when suppressed
	arrow string // "↑", "↓", or "" when suppressed
}

// buildRows renders alerts into tableRow values, applying the first-fire guard
// (Delta == 0 || Delta == CurrentPrice → blank delta+arrow cells).
func buildRows(alerts []alert) []tableRow {
	rows := make([]tableRow, len(alerts))
	for i, a := range alerts {
		row := tableRow{
			pair:  pairLabel(a),
			value: labelfmt.GroupThousands(a.CurrentPrice),
		}
		if a.Delta != 0 && a.Delta != a.CurrentPrice {
			if a.Delta > 0 {
				row.delta = fmt.Sprintf("+%s", labelfmt.GroupThousands(a.Delta))
				row.arrow = arrowUp
			} else {
				row.delta = fmt.Sprintf("%s%s", minusSign, labelfmt.GroupThousands(-a.Delta))
				row.arrow = arrowDown
			}
		}
		rows[i] = row
	}
	return rows
}

// renderBlock formats a slice of tableRow values into an aligned text block
// ready to wrap in <pre>…</pre>. Column separator is 2 spaces. Widths are
// computed by rune count (not bytes) so multibyte characters align correctly.
// Trailing whitespace is trimmed from each line.
func renderBlock(rows []tableRow) string {
	if len(rows) == 0 {
		return ""
	}
	var pairW, valueW, deltaW int
	for _, r := range rows {
		if w := utf8.RuneCountInString(r.pair); w > pairW {
			pairW = w
		}
		if w := utf8.RuneCountInString(r.value); w > valueW {
			valueW = w
		}
		if w := utf8.RuneCountInString(r.delta); w > deltaW {
			deltaW = w
		}
	}

	var sb strings.Builder
	for _, r := range rows {
		pairPad := pairW - utf8.RuneCountInString(r.pair)
		valuePad := valueW - utf8.RuneCountInString(r.value)
		deltaPad := deltaW - utf8.RuneCountInString(r.delta)

		line := r.pair + strings.Repeat(" ", pairPad) +
			"  " + strings.Repeat(" ", valuePad) + r.value +
			"  " + strings.Repeat(" ", deltaPad) + r.delta +
			" " + r.arrow
		sb.WriteString(strings.TrimRight(line, " "))
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

// reasonBadge computes the header badge fragment for a set of alerts.
// It returns a string such as "Δ ⏰ " (both), "Δ " (delta-only), "⏰ "
// (schedule-only), or "" (none). The trailing space is included so callers
// can concatenate directly with "🕒".
func reasonBadge(partAlerts []alert) string {
	var hasDelta, hasSched bool
	for _, a := range partAlerts {
		for _, tr := range a.Triggers {
			switch tr.ConditionType {
			case domain.ConditionTypeDelta:
				hasDelta = true
			case domain.ConditionTypeInterval, domain.ConditionTypeDaily, domain.ConditionTypeCron:
				hasSched = true
			}
		}
	}
	var sb strings.Builder
	if hasDelta {
		sb.WriteString(badgeIconDelta)
		sb.WriteByte(' ')
	}
	if hasSched {
		sb.WriteString(badgeIconSchedule)
		sb.WriteByte(' ')
	}
	return sb.String()
}

// headerLines returns the two header lines for a message part, using now as the
// timestamp and the given alerts to compute the badge.
func headerLines(now time.Time, partAlerts []alert) string {
	ts := now.UTC().Format("Mon 2 Jan, 15:04 UTC")
	badge := reasonBadge(partAlerts)
	return "📊 FX rates\n" + badge + "🕒 " + ts
}

// splitIntoParts packs rows into message parts bounded by telegramMaxMessageLen,
// re-emitting the header and a balanced <pre>…</pre> block per part.
// Widths are recomputed per part so each part is tightly aligned.
// A single row that would alone exceed the limit is still emitted as its own
// part (Telegram will reject it, but the loop must not spin forever).
func splitIntoParts(now time.Time, rows []tableRow, alerts []alert) []string {
	var parts []string
	start := 0

	for start < len(rows) {
		end := start + 1 // always include at least one row
		for end < len(rows) {
			candidate := buildPart(now, rows[start:end+1], alerts[start:end+1])
			if len(candidate) > telegramMaxMessageLen {
				break
			}
			end++
		}
		parts = append(parts, buildPart(now, rows[start:end], alerts[start:end]))
		start = end
	}
	return parts
}

// buildPart assembles one complete message part for the given row and alert slices.
func buildPart(now time.Time, rows []tableRow, partAlerts []alert) string {
	header := headerLines(now, partAlerts)
	block := renderBlock(rows)
	return header + "\n\n<pre>\n" + block + "\n</pre>"
}
