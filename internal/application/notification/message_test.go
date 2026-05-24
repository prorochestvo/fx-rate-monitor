package notification

import (
	"strings"
	"testing"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/stretchr/testify/require"
)

// fixedNow is a deterministic timestamp used in all header-format assertions.
// "Sat 23 May, 03:00 UTC" — a Saturday, UTC midnight-ish.
var fixedNow = time.Date(2026, time.May, 23, 3, 0, 0, 0, time.UTC)

func TestBuildAlertMessage(t *testing.T) {
	t.Parallel()

	t.Run("no alerts — empty slice", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow)
		require.NoError(t, err)
		require.Empty(t, msgs)
	})

	t.Run("BID pair rendered as base/quote", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			CurrencyKind:  domain.RateSourceKindBID,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.Contains(t, msgs[0], "USD/KZT")
		require.NotContains(t, msgs[0], "KZT/USD")
	})

	t.Run("ASK pair inverted to quote/base", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			CurrencyKind:  domain.RateSourceKindASK,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.Contains(t, msgs[0], "KZT/USD")
		require.NotContains(t, msgs[0], "USD/KZT")
	})

	t.Run("delta zero — blank delta cell, no 0.00 and no arrow", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			Delta:         0,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.NotContains(t, msgs[0], "0.00")
		require.NotContains(t, msgs[0], arrowUp)
		require.NotContains(t, msgs[0], arrowDown)
	})

	t.Run("delta equals CurrentPrice (first-fire) — blank delta cell", func(t *testing.T) {
		t.Parallel()

		const price = 470.46
		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  price,
			Delta:         price, // first-fire: LatestNotifiedRate was 0
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		// The price itself appears, but no "+470.46" or arrow.
		require.NotContains(t, msgs[0], "+")
		require.NotContains(t, msgs[0], arrowUp)
		require.NotContains(t, msgs[0], arrowDown)
	})

	t.Run("positive delta — ASCII + and up arrow", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  100,
			Delta:         1.5,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.Contains(t, msgs[0], "+1.50")
		require.Contains(t, msgs[0], arrowUp)
		require.NotContains(t, msgs[0], arrowDown)
	})

	t.Run("negative delta — U+2212 minus and down arrow", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  100,
			Delta:         -1.5,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.Contains(t, msgs[0], minusSign+"1.50")
		require.Contains(t, msgs[0], arrowDown)
		require.NotContains(t, msgs[0], arrowUp)
	})

	t.Run("thousands grouping survives inside pre block", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "GOLD",
			QuoteCurrency: "KZT",
			CurrentPrice:  68382.56,
			Delta:         -74.79,
			CurrencyKind:  domain.RateSourceKindBID,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		// Assert exact substring so the ASCII-space byte (0x20) is verified.
		require.Contains(t, msgs[0], "68 382.56")
		require.Contains(t, msgs[0], "<pre>")
		require.Contains(t, msgs[0], "</pre>")
	})

	t.Run("column alignment with varied widths", func(t *testing.T) {
		t.Parallel()

		// Two rows with very different pair and value widths; one has a suppressed
		// delta (first-fire). Assert the exact block to catch alignment regressions.
		msgs, err := buildAlertMessage(fixedNow,
			alert{
				BaseCurrency:  "USD",
				QuoteCurrency: "KZT",
				CurrentPrice:  471.95,
				Delta:         2.60,
				CurrencyKind:  domain.RateSourceKindBID,
			},
			alert{
				BaseCurrency:  "GOLD",
				QuoteCurrency: "KZT",
				CurrentPrice:  68382.56,
				Delta:         68382.56, // first-fire — suppressed
				CurrencyKind:  domain.RateSourceKindBID,
			},
		)
		require.NoError(t, err)
		require.Len(t, msgs, 1)

		// Pair column width = max("GOLD/KZT"=8, "USD/KZT"=7) = 8.
		// Value column width = max("68 382.56"=9, "471.95"=6) = 9.
		// Delta column for USD/KZT: "+2.60" = 5; GOLD/KZT: "" = 0; deltaW = 5.
		// Layout: pair(left,8)  value(right,9)  delta(right,5) arrow
		// Sorted by pair: GOLD/KZT comes before USD/KZT.
		// GOLD/KZT: delta suppressed → trailing spaces trimmed by TrimRight → no trailing content.
		// USD/KZT:  pair(7)+1pad=8, value(6)+3pad=9, delta "+2.60" + " ↑"
		// Actual: "GOLD/KZT  68 382.56\nUSD/KZT      471.95  +2.60 ↑"
		expectedBlock := "GOLD/KZT  68 382.56\nUSD/KZT      471.95  +2.60 ↑"
		require.Contains(t, msgs[0], expectedBlock)
	})

	t.Run("header format — line 1 and line 2 with fixed now", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			Delta:         1.20,
			Triggers: []alertTrigger{
				{ConditionType: domain.ConditionTypeDelta, ConditionValue: "1"},
			},
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.True(t, strings.HasPrefix(msgs[0], "📊 FX rates\n"))
		require.Contains(t, msgs[0], "🕒 Sat 23 May, 03:00 UTC")
	})

	t.Run("badge delta-only", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  100,
			Delta:         1,
			Triggers: []alertTrigger{
				{ConditionType: domain.ConditionTypeDelta, ConditionValue: "0.5"},
			},
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.Contains(t, msgs[0], badgeIconDelta+" 🕒")
		require.NotContains(t, msgs[0], badgeIconSchedule)
	})

	t.Run("badge schedule-only (interval)", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  100,
			Delta:         1,
			Triggers: []alertTrigger{
				{ConditionType: domain.ConditionTypeInterval, ConditionValue: "4h"},
			},
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.Contains(t, msgs[0], badgeIconSchedule+" 🕒")
		require.NotContains(t, msgs[0], badgeIconDelta)
	})

	t.Run("badge schedule-only (daily)", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  100,
			Delta:         1,
			Triggers: []alertTrigger{
				{ConditionType: domain.ConditionTypeDaily, ConditionValue: "06:00:00"},
			},
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.Contains(t, msgs[0], badgeIconSchedule+" 🕒")
		require.NotContains(t, msgs[0], badgeIconDelta)
	})

	t.Run("badge schedule-only (cron)", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  100,
			Delta:         1,
			Triggers: []alertTrigger{
				{ConditionType: domain.ConditionTypeCron, ConditionValue: "0 9 * * 1"},
			},
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.Contains(t, msgs[0], badgeIconSchedule+" 🕒")
		require.NotContains(t, msgs[0], badgeIconDelta)
	})

	t.Run("badge mixed time-types collapse to one schedule glyph", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  100,
			Delta:         1,
			Triggers: []alertTrigger{
				{ConditionType: domain.ConditionTypeInterval, ConditionValue: "4h"},
				{ConditionType: domain.ConditionTypeDaily, ConditionValue: "06:00:00"},
				{ConditionType: domain.ConditionTypeCron, ConditionValue: "0 9 * * 1"},
			},
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		// Only one schedule glyph.
		require.Equal(t, 1, strings.Count(msgs[0], badgeIconSchedule))
		require.NotContains(t, msgs[0], badgeIconDelta)
	})

	t.Run("badge both delta and schedule", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  100,
			Delta:         1,
			Triggers: []alertTrigger{
				{ConditionType: domain.ConditionTypeDelta, ConditionValue: "0.5"},
				{ConditionType: domain.ConditionTypeInterval, ConditionValue: "4h"},
			},
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.Contains(t, msgs[0], badgeIconDelta+" "+badgeIconSchedule+" 🕒")
	})

	t.Run("badge none — no leading glyph before clock", func(t *testing.T) {
		t.Parallel()

		// No triggers at all — badge is empty, clock directly follows newline.
		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.Contains(t, msgs[0], "\n🕒 ")
		require.NotContains(t, msgs[0], badgeIconDelta)
		require.NotContains(t, msgs[0], badgeIconSchedule)
	})

	t.Run("each message part contains one balanced pre block", func(t *testing.T) {
		t.Parallel()

		// Verify the <pre>…</pre> structure for a single part.
		msgs, err := buildAlertMessage(fixedNow, alert{
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			Delta:         1.5,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.Equal(t, 1, strings.Count(msgs[0], "<pre>"), "exactly one <pre> open tag")
		require.Equal(t, 1, strings.Count(msgs[0], "</pre>"), "exactly one </pre> close tag")
		preIdx := strings.Index(msgs[0], "<pre>")
		postIdx := strings.Index(msgs[0], "</pre>")
		require.True(t, preIdx < postIdx, "<pre> must precede </pre>")
	})

	t.Run("splitting forces multiple parts each with balanced pre", func(t *testing.T) {
		t.Parallel()

		// Generate enough rows to overflow telegramMaxMessageLen (2048 bytes).
		// Each row is roughly "AAAA/KZT    999.99  +99.99 ↑\n" ≈ 35 chars.
		// Header+wrapper is ≈ 100 bytes. We need > (2048-100)/35 ≈ 56 rows.
		alerts := make([]alert, 80)
		for i := range alerts {
			// Use unique base currencies by encoding the index into four letters
			// to ensure each row is distinct.
			base := string([]rune{
				rune('A' + i/26),
				rune('A' + i%26),
				'X',
				rune('A' + i%5),
			})
			alerts[i] = alert{
				BaseCurrency:  base,
				QuoteCurrency: "KZT",
				CurrentPrice:  float64(100 + i),
				Delta:         float64(i + 1),
				CurrencyKind:  domain.RateSourceKindBID,
			}
		}

		msgs, err := buildAlertMessage(fixedNow, alerts...)
		require.NoError(t, err)
		require.Greater(t, len(msgs), 1, "expected at least 2 parts")

		seen := make(map[string]bool)
		for _, msg := range msgs {
			// Each part must have exactly one balanced <pre>…</pre>.
			require.Equal(t, 1, strings.Count(msg, "<pre>"))
			require.Equal(t, 1, strings.Count(msg, "</pre>"))
			require.True(t, strings.Index(msg, "<pre>") < strings.Index(msg, "</pre>"))

			// Collect pair strings to verify each row appears exactly once.
			preStart := strings.Index(msg, "<pre>\n") + len("<pre>\n")
			preEnd := strings.Index(msg, "\n</pre>")
			if preStart > 0 && preEnd > preStart {
				block := msg[preStart:preEnd]
				for _, line := range strings.Split(block, "\n") {
					if line == "" {
						continue
					}
					fields := strings.Fields(line)
					if len(fields) > 0 {
						pair := fields[0]
						require.False(t, seen[pair], "pair %q appears more than once across parts", pair)
						seen[pair] = true
					}
				}
			}
		}
		// All 80 rows must appear exactly once.
		require.Len(t, seen, 80)
	})
}

func TestReasonBadge(t *testing.T) {
	t.Parallel()

	t.Run("delta-only", func(t *testing.T) {
		t.Parallel()
		alerts := []alert{{Triggers: []alertTrigger{{ConditionType: domain.ConditionTypeDelta}}}}
		require.Equal(t, badgeIconDelta+" ", reasonBadge(alerts))
	})
	t.Run("interval-only", func(t *testing.T) {
		t.Parallel()
		alerts := []alert{{Triggers: []alertTrigger{{ConditionType: domain.ConditionTypeInterval}}}}
		require.Equal(t, badgeIconSchedule+" ", reasonBadge(alerts))
	})
	t.Run("daily-only", func(t *testing.T) {
		t.Parallel()
		alerts := []alert{{Triggers: []alertTrigger{{ConditionType: domain.ConditionTypeDaily}}}}
		require.Equal(t, badgeIconSchedule+" ", reasonBadge(alerts))
	})
	t.Run("cron-only", func(t *testing.T) {
		t.Parallel()
		alerts := []alert{{Triggers: []alertTrigger{{ConditionType: domain.ConditionTypeCron}}}}
		require.Equal(t, badgeIconSchedule+" ", reasonBadge(alerts))
	})
	t.Run("mixed time types collapse to one schedule glyph", func(t *testing.T) {
		t.Parallel()
		alerts := []alert{{Triggers: []alertTrigger{
			{ConditionType: domain.ConditionTypeInterval},
			{ConditionType: domain.ConditionTypeDaily},
			{ConditionType: domain.ConditionTypeCron},
		}}}
		require.Equal(t, badgeIconSchedule+" ", reasonBadge(alerts))
	})
	t.Run("delta and schedule both present", func(t *testing.T) {
		t.Parallel()
		alerts := []alert{{Triggers: []alertTrigger{
			{ConditionType: domain.ConditionTypeDelta},
			{ConditionType: domain.ConditionTypeInterval},
		}}}
		require.Equal(t, badgeIconDelta+" "+badgeIconSchedule+" ", reasonBadge(alerts))
	})
	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "", reasonBadge(nil))
		require.Equal(t, "", reasonBadge([]alert{{}}))
	})
}
