package notification

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildAlertMessage(t *testing.T) {
	t.Parallel()

	t.Run("single alert produces one message", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
			SourceTitle:   "National Bank",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			CurrentPrice:  470.46,
		})
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		require.True(t, strings.Contains(msgs[0], "USD/KZT"))
	})
	t.Run("delta zero — no arrow in message", func(t *testing.T) {
		t.Parallel()

		msgs, err := buildAlertMessage(alert{
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
}
