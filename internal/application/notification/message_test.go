package notification

import (
	"strings"
	"testing"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestBuildAlertMessage(t *testing.T) {
	t.Parallel()

	t.Run("single alert produces one message", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:   "A Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			CurrencyKind:  domain.RateSourceKindBID,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.True(t, strings.Contains(msgs[0], "USD/KZT"))

		msgs, err = buildAlertMessage(alert{
			SourceTitle:   "B Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			CurrencyKind:  domain.RateSourceKindASK,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.True(t, strings.Contains(msgs[0], "KZT/USD"))
	})
	t.Run("delta zero — no arrow in message", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:   "Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			Delta:         470.46,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.False(t, strings.Contains(msgs[0], telegramBotArrowUp))
		require.False(t, strings.Contains(msgs[0], telegramBotArrowDown))

		msgs, err = buildAlertMessage(alert{
			SourceTitle:   "Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			Delta:         0,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.False(t, strings.Contains(msgs[0], telegramBotArrowUp))
		require.False(t, strings.Contains(msgs[0], telegramBotArrowDown))
	})
	t.Run("positive delta — up arrow shown", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:   "Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  100,
			Delta:         1.5,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.True(t, strings.Contains(msgs[0], telegramBotArrowUp))
	})
	t.Run("negative delta — down arrow shown", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:   "Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  100,
			Delta:         -1.5,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.True(t, strings.Contains(msgs[0], telegramBotArrowDown))
	})
	t.Run("forecast shown when ForecastMethod non-empty", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:    "Bank",
			BaseCurrency:   "USD",
			QuoteCurrency:  "KZT",
			CurrentPrice:   100,
			ForecastMethod: "composite",
			ForecastPrice:  99.9,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.True(t, strings.Contains(msgs[0], telegramBotForecast))
	})
	t.Run("forecast absent when ForecastMethod empty", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:    "Bank",
			BaseCurrency:   "USD",
			QuoteCurrency:  "KZT",
			CurrentPrice:   100,
			ForecastMethod: "",
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.False(t, strings.Contains(msgs[0], telegramBotForecast))
	})
	t.Run("no alerts — empty slice", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage()
		require.NoError(t, err)
		require.Len(t, msgs, 0)
	})
	t.Run("messages split above 2048 chars", func(t *testing.T) {
		t.Parallel()

		alerts := make([]alert, 50)
		for i := range alerts {
			alerts[i] = alert{
				SourceTitle:   strings.Repeat("X", 40) + string(rune('A'+i%26)),
				BaseCurrency:  "USD",
				QuoteCurrency: "KZT",
				CurrentPrice:  float64(400 + i),
			}
		}

		msgs, err := buildAlertMessage(alerts...)
		require.NoError(t, err)
		require.Greater(t, len(msgs), 1)
		require.Equal(t, 2, len(msgs))
	})
	t.Run("empty triggers — output unchanged", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:   "Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			CurrencyKind:  domain.RateSourceKindBID,
			Delta:         1.20,
			Triggers:      nil,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.False(t, strings.Contains(msgs[0], triggerIconDelta))
		require.False(t, strings.Contains(msgs[0], triggerIconInterval))
		require.False(t, strings.Contains(msgs[0], triggerIconDaily))
		require.False(t, strings.Contains(msgs[0], triggerIconCron))
	})
	t.Run("single delta trigger renders icon after price block", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:   "Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			CurrencyKind:  domain.RateSourceKindBID,
			Delta:         1.20,
			Triggers: []alertTrigger{
				{ConditionType: domain.ConditionTypeDelta, ConditionValue: "5"},
			},
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.Contains(t, msgs[0], triggerIconDelta+" ≥5%")
	})
	t.Run("multi-trigger alert renders all four icons in stable order", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:   "Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
			CurrencyKind:  domain.RateSourceKindBID,
			Triggers: []alertTrigger{
				{ConditionType: domain.ConditionTypeDelta, ConditionValue: "10"},
				{ConditionType: domain.ConditionTypeInterval, ConditionValue: "4h"},
				{ConditionType: domain.ConditionTypeDaily, ConditionValue: "06:00:00"},
				{ConditionType: domain.ConditionTypeCron, ConditionValue: "0 9 * * 1"},
			},
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		msg := msgs[0]
		require.Contains(t, msg, triggerIconDelta)
		require.Contains(t, msg, triggerIconInterval)
		require.Contains(t, msg, triggerIconDaily)
		require.Contains(t, msg, triggerIconCron)

		dPos := strings.Index(msg, triggerIconDelta)
		iPos := strings.Index(msg, triggerIconInterval)
		dailyPos := strings.Index(msg, triggerIconDaily)
		cronPos := strings.Index(msg, triggerIconCron)
		require.True(t, dPos < iPos)
		require.True(t, iPos < dailyPos)
		require.True(t, dailyPos < cronPos)
	})
	t.Run("forecast and triggers combine cleanly", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:    "Bank",
			BaseCurrency:   "USD",
			QuoteCurrency:  "KZT",
			CurrentPrice:   470.46,
			CurrencyKind:   domain.RateSourceKindBID,
			ForecastMethod: "composite",
			ForecastPrice:  472.0,
			Triggers: []alertTrigger{
				{ConditionType: domain.ConditionTypeDelta, ConditionValue: "5"},
			},
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		msg := msgs[0]
		require.Contains(t, msg, telegramBotForecast)
		require.Contains(t, msg, triggerIconDelta)
		forecastPos := strings.Index(msg, telegramBotForecast)
		triggerPos := strings.Index(msg, triggerIconDelta)
		require.True(t, forecastPos < triggerPos, "forecast must appear before trigger icons")
	})
}

func TestTriggerLabel(t *testing.T) {
	t.Parallel()

	t.Run("delta condition", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, triggerIconDelta+" ≥10%", triggerLabel(domain.ConditionTypeDelta, "10"))
	})
	t.Run("interval condition", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, triggerIconInterval+" 4h", triggerLabel(domain.ConditionTypeInterval, "4h"))
		require.Equal(t, triggerIconInterval+" 1d", triggerLabel(domain.ConditionTypeInterval, "24h"))
		require.Equal(t, triggerIconInterval+" 1w", triggerLabel(domain.ConditionTypeInterval, "168h"))
	})
	t.Run("daily condition", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, triggerIconDaily+" 06:00", triggerLabel(domain.ConditionTypeDaily, "06:00:00"))
		require.Equal(t, triggerIconDaily+" 06", triggerLabel(domain.ConditionTypeDaily, "06"))
	})
	t.Run("cron condition", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, triggerIconCron+" Mon", triggerLabel(domain.ConditionTypeCron, "0 9 * * 1"))
	})
	t.Run("unknown condition type returns empty string", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "", triggerLabel("unknown", "val"))
	})
}
