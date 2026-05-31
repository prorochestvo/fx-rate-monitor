package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seilbekskindirov/monitor/cmd/wasm/application"
	"github.com/seilbekskindirov/monitor/cmd/wasm/ui"
	"github.com/seilbekskindirov/monitor/internal/domain/ratepair"
	"github.com/seilbekskindirov/monitor/internal/dto"
)

// historyState is a convenience helper that builds a MeSubscriptionsState with
// HistoryOpen=true and the supplied history fields.
func historyState(page, limit int, total int64, items []dto.MeHistoryRow, loading bool, err error) application.MeSubscriptionsState {
	pair := "USD/KZT"
	return application.MeSubscriptionsState{
		OpenPair:       &pair,
		HistoryOpen:    true,
		HistoryPage:    page,
		HistoryLimit:   limit,
		HistoryTotal:   total,
		HistoryItems:   items,
		HistoryLoading: loading,
		HistoryError:   err,
	}
}

// bidRow builds a MeHistoryRow with only Bid set.
func bidRow(sourceName, title string, ts time.Time, bid float64, bidDelta *float64) dto.MeHistoryRow {
	return dto.MeHistoryRow{
		SourceName:  sourceName,
		SourceTitle: title,
		Timestamp:   ts,
		Bid:         &bid,
		BidDeltaPct: bidDelta,
	}
}

// askRow builds a MeHistoryRow with only Ask set.
func askRow(sourceName, title string, ts time.Time, ask float64, askDelta *float64) dto.MeHistoryRow {
	return dto.MeHistoryRow{
		SourceName:  sourceName,
		SourceTitle: title,
		Timestamp:   ts,
		Ask:         &ask,
		AskDeltaPct: askDelta,
	}
}

// bidAskRow builds a MeHistoryRow with both Bid and Ask set.
func bidAskRow(sourceName, title string, ts time.Time, bid, ask float64, bidDelta, askDelta *float64) dto.MeHistoryRow {
	return dto.MeHistoryRow{
		SourceName:  sourceName,
		SourceTitle: title,
		Timestamp:   ts,
		Bid:         &bid,
		Ask:         &ask,
		BidDeltaPct: bidDelta,
		AskDeltaPct: askDelta,
	}
}

func float64PtrH(v float64) *float64 { return &v }

func TestRenderPairHistory(t *testing.T) {
	t.Parallel()

	t.Run("empty items renders no-history empty state", func(t *testing.T) {
		t.Parallel()
		state := historyState(1, 20, 0, nil, false, nil)
		html := ui.RenderPairHistory(state)

		assert.Contains(t, html, "me-pair-history-empty")
		assert.Contains(t, html, "No history yet")
		assert.NotContains(t, html, "me-pair-history-entry")
	})

	t.Run("loading state renders skeleton and no entries", func(t *testing.T) {
		t.Parallel()
		state := historyState(1, 20, 0, nil, true, nil)
		html := ui.RenderPairHistory(state)

		assert.Contains(t, html, "me-pair-history-loading")
		assert.Contains(t, html, "Loading")
		assert.NotContains(t, html, "me-pair-history-entry")
		assert.NotContains(t, html, "me-pair-history-empty")
	})

	t.Run("error state renders humane generic message", func(t *testing.T) {
		t.Parallel()
		state := historyState(1, 20, 0, nil, false, &testErr{"history unavailable"})
		html := ui.RenderPairHistory(state)

		assert.Contains(t, html, "me-pair-history-error")
		assert.Contains(t, html, "Could not load history. Try again.")
		// Raw error text must not leak into the output.
		assert.NotContains(t, html, "history unavailable")
		assert.NotContains(t, html, "me-pair-history-empty")
		assert.NotContains(t, html, "me-pair-history-entry")
	})

	t.Run("error state with auth failure sentinel renders auth-failure copy", func(t *testing.T) {
		t.Parallel()
		state := historyState(1, 20, 0, nil, false, &testErr{"http 401 unauthorized"})
		html := ui.RenderPairHistory(state)

		assert.Contains(t, html, "me-pair-history-error")
		assert.Contains(t, html, "opened from the bot")
		assert.NotContains(t, html, "http 401")
	})

	t.Run("single-direction entry renders BID only", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		items := []dto.MeHistoryRow{bidRow("kkb", "Kaspi", ts, 487.5, nil)}
		state := historyState(1, 20, 1, items, false, nil)
		html := ui.RenderPairHistory(state)

		assert.Contains(t, html, "me-pair-history-bid")
		assert.Contains(t, html, "BID")
		assert.Contains(t, html, "487.5")
		assert.NotContains(t, html, "me-pair-history-ask")
	})

	t.Run("single-direction entry renders ASK only", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		items := []dto.MeHistoryRow{askRow("kkb", "Kaspi", ts, 489.0, nil)}
		state := historyState(1, 20, 1, items, false, nil)
		html := ui.RenderPairHistory(state)

		assert.Contains(t, html, "me-pair-history-ask")
		assert.Contains(t, html, "ASK")
		assert.Contains(t, html, "489")
		assert.NotContains(t, html, "me-pair-history-bid")
	})

	t.Run("two-direction entry renders BID and ASK", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		items := []dto.MeHistoryRow{bidAskRow("kkb", "Kaspi", ts, 487.5, 489.0, nil, nil)}
		state := historyState(1, 20, 1, items, false, nil)
		html := ui.RenderPairHistory(state)

		assert.Contains(t, html, "me-pair-history-bid")
		assert.Contains(t, html, "me-pair-history-ask")
		assert.Contains(t, html, "BID")
		assert.Contains(t, html, "ASK")
		assert.Contains(t, html, "487.5")
		assert.Contains(t, html, "489")
	})

	t.Run("nil delta renders em-dash", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		items := []dto.MeHistoryRow{bidRow("kkb", "Kaspi", ts, 487.5, nil)}
		state := historyState(1, 20, 1, items, false, nil)
		html := ui.RenderPairHistory(state)

		// Em-dash is &#8212; in HTML — assert the rendered entity or its unicode form.
		assert.True(t,
			strings.Contains(html, "&#8212;") || strings.Contains(html, "—"),
			"nil delta must render as em-dash",
		)
	})

	t.Run("positive delta is colored up", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		delta := 1.23
		items := []dto.MeHistoryRow{bidRow("kkb", "Kaspi", ts, 487.5, &delta)}
		state := historyState(1, 20, 1, items, false, nil)
		html := ui.RenderPairHistory(state)

		assert.Contains(t, html, ratepair.ColorDeltaUp)
		assert.Contains(t, html, "+1.23%")
		assert.NotContains(t, html, ratepair.ColorDeltaDown)
	})

	t.Run("negative delta is colored down", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		delta := -0.75
		items := []dto.MeHistoryRow{bidRow("kkb", "Kaspi", ts, 487.5, &delta)}
		state := historyState(1, 20, 1, items, false, nil)
		html := ui.RenderPairHistory(state)

		assert.Contains(t, html, ratepair.ColorDeltaDown)
		assert.Contains(t, html, "-0.75%")
		assert.NotContains(t, html, ratepair.ColorDeltaUp)
	})

	t.Run("pagination prev disabled on page 1", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		items := []dto.MeHistoryRow{bidRow("kkb", "Kaspi", ts, 487.5, nil)}
		state := historyState(1, 20, 100, items, false, nil)
		html := ui.RenderPairHistory(state)

		prevTag := buttonTag(t, html, "me-pair-history-prev")
		require.NotEmpty(t, prevTag, "prev button must be present")
		assert.Contains(t, prevTag, "disabled", "prev must be disabled on page 1")

		nextTag := buttonTag(t, html, "me-pair-history-next")
		require.NotEmpty(t, nextTag, "next button must be present")
		assert.NotContains(t, nextTag, "disabled", "next must be enabled when more pages exist")
	})

	t.Run("pagination next disabled on last page", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		items := []dto.MeHistoryRow{bidRow("kkb", "Kaspi", ts, 487.5, nil)}
		// Page 5, limit 20, total 100 — page*limit == total, so at end.
		state := historyState(5, 20, 100, items, false, nil)
		html := ui.RenderPairHistory(state)

		nextTag := buttonTag(t, html, "me-pair-history-next")
		require.NotEmpty(t, nextTag)
		assert.Contains(t, nextTag, "disabled", "next must be disabled on last page")

		prevTag := buttonTag(t, html, "me-pair-history-prev")
		require.NotEmpty(t, prevTag)
		assert.NotContains(t, prevTag, "disabled", "prev must be enabled on non-first page")
	})

	t.Run("XSS in source title is escaped", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		hostile := `<script>alert(1)</script>`
		items := []dto.MeHistoryRow{bidRow("src", hostile, ts, 487.5, nil)}
		state := historyState(1, 20, 1, items, false, nil)
		html := ui.RenderPairHistory(state)

		assert.NotContains(t, html, "<script>")
		assert.Contains(t, html, "&lt;script&gt;")
	})

	t.Run("XSS in source name is escaped", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		hostile := `"><img src=x onerror=alert(1)>`
		items := []dto.MeHistoryRow{bidRow(hostile, "Kaspi", ts, 487.5, nil)}
		state := historyState(1, 20, 1, items, false, nil)
		html := ui.RenderPairHistory(state)

		// The source name is in me-pair-history-head; img must not appear.
		assert.NotContains(t, html, "<img src=x")
	})

	t.Run("no script tag in output", func(t *testing.T) {
		t.Parallel()
		ts := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
		items := []dto.MeHistoryRow{
			bidAskRow("kkb", "Kaspi", ts, 487.5, 489.0, float64PtrH(1.0), float64PtrH(-0.5)),
		}
		state := historyState(2, 20, 100, items, false, nil)
		html := ui.RenderPairHistory(state)
		assert.NotContains(t, strings.ToLower(html), "<script")
	})

	t.Run("back button is present", func(t *testing.T) {
		t.Parallel()
		state := historyState(1, 20, 0, nil, false, nil)
		html := ui.RenderPairHistory(state)
		assert.Contains(t, html, `id="me-pair-history-back"`)
	})
}

// buttonTag extracts the full opening tag (from < to >) that contains the
// given id value. Used to check disabled state without parsing HTML.
func buttonTag(tb testing.TB, html, id string) string {
	tb.Helper()
	idx := strings.Index(html, `id="`+id+`"`)
	if idx < 0 {
		return ""
	}
	start := strings.LastIndex(html[:idx], "<")
	end := strings.Index(html[idx:], ">")
	if end < 0 {
		return html[start:]
	}
	return html[start : idx+end+1]
}

// testErr is a minimal error type for test use.
type testErr struct{ msg string }

func (e *testErr) Error() string { return e.msg }
