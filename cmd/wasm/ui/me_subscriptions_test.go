package ui_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/seilbekskindirov/monitor/cmd/wasm/application"
	"github.com/seilbekskindirov/monitor/cmd/wasm/ui"
	"github.com/seilbekskindirov/monitor/internal/dto"
)

func meSubsState(items []dto.MeSubscriptionRow, total int64, page, pageSize int) application.MeSubscriptionsState {
	return application.MeSubscriptionsState{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Period:   application.MeSubscriptionsPeriodWeek,
		Chart:    application.OverlayChartState{Errors: map[string]error{}},
	}
}

func singleItem() dto.MeSubscriptionRow {
	return dto.MeSubscriptionRow{
		SourceName:    "usd-eur",
		SourceTitle:   "USD/EUR",
		BaseCurrency:  "USD",
		QuoteCurrency: "EUR",
		Conditions:    []string{">1.05", "<2.00"},
		LatestPrice:   1.0812,
		LatestAt:      "2026-01-01T12:00:00Z",
	}
}

func stateWithSeries(items []dto.MeSubscriptionRow, series []application.SeriesData) application.MeSubscriptionsState {
	st := meSubsState(items, int64(len(items)), 1, 10)
	st.Chart = application.OverlayChartState{
		Loaded: true,
		Series: series,
		Errors: map[string]error{},
	}
	return st
}

func TestRenderMeSubscriptions(t *testing.T) {
	t.Parallel()

	t.Run("empty state renders no-subscriptions message", func(t *testing.T) {
		t.Parallel()
		state := meSubsState(nil, 0, 1, 10)
		html := ui.RenderMeSubscriptions(state)
		assert.Contains(t, html, "No subscriptions found.")
		assert.Contains(t, html, `class="search-bar"`)
		assert.NotContains(t, html, `class="pagination"`)
	})

	t.Run("pagination wrapper id is always present", func(t *testing.T) {
		t.Parallel()

		t.Run("happy path with items", func(t *testing.T) {
			t.Parallel()
			state := meSubsState([]dto.MeSubscriptionRow{singleItem()}, 1, 1, 10)
			html := ui.RenderMeSubscriptions(state)
			assert.Contains(t, html, `id="me-subs-pagination"`)
		})

		t.Run("empty state", func(t *testing.T) {
			t.Parallel()
			state := meSubsState(nil, 0, 1, 10)
			html := ui.RenderMeSubscriptions(state)
			assert.Contains(t, html, `id="me-subs-pagination"`)
		})

		t.Run("generic error", func(t *testing.T) {
			t.Parallel()
			state := meSubsState(nil, 0, 1, 10)
			state.LastError = errors.New("network timeout")
			html := ui.RenderMeSubscriptions(state)
			assert.Contains(t, html, `id="me-subs-pagination"`)
		})
	})

	t.Run("single card renders all fields", func(t *testing.T) {
		t.Parallel()
		state := meSubsState([]dto.MeSubscriptionRow{singleItem()}, 1, 1, 10)
		html := ui.RenderMeSubscriptions(state)

		assert.Contains(t, html, `class="card"`)
		assert.Contains(t, html, `class="card-title"`)
		assert.Contains(t, html, "USD/EUR")
		assert.Contains(t, html, `class="card-pair"`)
		assert.Contains(t, html, "USD/EUR")
		assert.Contains(t, html, `class="card-price"`)
		assert.Contains(t, html, "1.0812")
		assert.Contains(t, html, `class="card-time"`)
		assert.Contains(t, html, "Last grab:")
		assert.Contains(t, html, `class="badge"`)
		assert.Contains(t, html, "&gt;1.05")
		assert.Contains(t, html, "&lt;2.00")
	})

	t.Run("single card no pagination when only 1 page", func(t *testing.T) {
		t.Parallel()
		state := meSubsState([]dto.MeSubscriptionRow{singleItem()}, 1, 1, 10)
		html := ui.RenderMeSubscriptions(state)
		assert.NotContains(t, html, `class="pagination"`)
	})

	t.Run("multi-card with pagination shows prev and next buttons", func(t *testing.T) {
		t.Parallel()
		items := []dto.MeSubscriptionRow{
			singleItem(),
			{SourceName: "gbp-usd", SourceTitle: "GBP/USD", BaseCurrency: "GBP", QuoteCurrency: "USD", LatestPrice: 1.27},
		}
		// 25 total, page size 10, current page 2 → both prev and next shown
		state := meSubsState(items, 25, 2, 10)
		html := ui.RenderMeSubscriptions(state)

		assert.Contains(t, html, `class="pagination"`)
		assert.Contains(t, html, `data-section="me-subs"`)
		// page 2 → prev button navigates to page 1
		assert.Contains(t, html, `data-page="1"`)
		// page 2, count 2 (< pageSize 10) → no next button; len(items)==2 < limit=10
		assert.NotContains(t, html, `data-page="3"`)
	})

	t.Run("multi-card page 1 with full page shows next but no prev", func(t *testing.T) {
		t.Parallel()
		items := make([]dto.MeSubscriptionRow, 10)
		for i := range items {
			items[i] = singleItem()
		}
		state := meSubsState(items, 25, 1, 10)
		html := ui.RenderMeSubscriptions(state)

		assert.Contains(t, html, `class="pagination"`)
		assert.Contains(t, html, `data-page="2"`) // next page
		assert.NotContains(t, html, `data-page="0"`)
		// prev disabled because page == 1
		assert.Contains(t, html, `<button disabled>`)
	})

	t.Run("401 auth failure shows error message and hides chart and toggle", func(t *testing.T) {
		t.Parallel()
		state := meSubsState(nil, 0, 1, 10)
		state.AuthFailure = true
		html := ui.RenderMeSubscriptions(state)

		assert.Contains(t, html, "must be opened from the bot")
		assert.Contains(t, html, `class="error-msg"`)
		assert.NotContains(t, html, `class="pagination"`)
		assert.NotContains(t, html, "No subscriptions yet.")
		// Auth failure must hide chart and period toggle per Task 4.
		assert.NotContains(t, html, `id="me-period-toggle"`)
		assert.NotContains(t, html, `id="me-overlay-chart"`)
	})

	t.Run("generic error shows error message and hides pagination", func(t *testing.T) {
		t.Parallel()
		state := meSubsState(nil, 0, 1, 10)
		state.LastError = errors.New("network timeout")
		html := ui.RenderMeSubscriptions(state)

		assert.Contains(t, html, "Error loading subscriptions:")
		assert.Contains(t, html, "network timeout")
		assert.Contains(t, html, `class="error-msg"`)
		assert.NotContains(t, html, `class="pagination"`)
	})

	t.Run("XSS payload in source_title is escaped", func(t *testing.T) {
		t.Parallel()
		item := dto.MeSubscriptionRow{
			SourceName:    "evil-source",
			SourceTitle:   `<script>alert(1)</script>`,
			BaseCurrency:  "USD",
			QuoteCurrency: "EUR",
			Conditions:    []string{`<img src=x onerror=alert(1)>`, `A & B > C`},
			LatestPrice:   1.0,
		}
		state := meSubsState([]dto.MeSubscriptionRow{item}, 1, 1, 10)
		html := ui.RenderMeSubscriptions(state)

		assert.NotContains(t, html, "<script>", "raw <script> must not appear")
		assert.NotContains(t, html, "alert(1)</script>")
		assert.Contains(t, html, "&lt;script&gt;")
		assert.Contains(t, html, "&lt;img src=x onerror=alert(1)&gt;")
		assert.Contains(t, html, "A &amp; B &gt; C")
	})

	t.Run("source_name used as fallback when source_title is empty", func(t *testing.T) {
		t.Parallel()
		item := dto.MeSubscriptionRow{
			SourceName:    "my-source",
			SourceTitle:   "",
			BaseCurrency:  "USD",
			QuoteCurrency: "EUR",
		}
		state := meSubsState([]dto.MeSubscriptionRow{item}, 1, 1, 10)
		html := ui.RenderMeSubscriptions(state)
		assert.Contains(t, html, "my-source")
	})

	t.Run("missing latest_price renders dash", func(t *testing.T) {
		t.Parallel()
		item := dto.MeSubscriptionRow{
			SourceName:    "s",
			SourceTitle:   "S",
			BaseCurrency:  "USD",
			QuoteCurrency: "EUR",
			LatestPrice:   0,
		}
		state := meSubsState([]dto.MeSubscriptionRow{item}, 1, 1, 10)
		html := ui.RenderMeSubscriptions(state)
		assert.Contains(t, html, `class="card-price"`)
		assert.Contains(t, html, "—")
	})

	t.Run("search bar renders with current query value", func(t *testing.T) {
		t.Parallel()
		state := meSubsState(nil, 0, 1, 10)
		state.Query = "usd"
		html := ui.RenderMeSubscriptions(state)
		assert.Contains(t, html, `value="usd"`)
	})

	t.Run("XSS in query value is escaped in search bar", func(t *testing.T) {
		t.Parallel()
		state := meSubsState(nil, 0, 1, 10)
		state.Query = `"><script>alert(1)</script>`
		html := ui.RenderMeSubscriptions(state)
		assert.NotContains(t, html, "<script>")
		assert.Contains(t, html, "&lt;script&gt;")
	})

	t.Run("period toggle renders three buttons with current period active", func(t *testing.T) {
		t.Parallel()
		state := meSubsState(nil, 0, 1, 10)
		state.Period = application.MeSubscriptionsPeriodMonth
		html := ui.RenderMeSubscriptions(state)

		assert.Contains(t, html, `id="me-period-toggle"`)
		assert.Contains(t, html, `data-period="week"`)
		assert.Contains(t, html, `data-period="month"`)
		assert.Contains(t, html, `data-period="year"`)
		// Month button must have "active" class.
		assert.Contains(t, html, `class="period-btn active" data-period="month"`)
		// Week and year must not be active.
		assert.NotContains(t, html, `class="period-btn active" data-period="week"`)
		assert.NotContains(t, html, `class="period-btn active" data-period="year"`)
	})

	t.Run("list section is hidden by default", func(t *testing.T) {
		t.Parallel()
		state := meSubsState(nil, 0, 1, 10)
		state.ListVisible = false
		html := ui.RenderMeSubscriptions(state)
		assert.Contains(t, html, `id="me-subs-section" hidden`)
	})

	t.Run("list section is visible when ListVisible=true", func(t *testing.T) {
		t.Parallel()
		state := meSubsState(nil, 0, 1, 10)
		state.ListVisible = true
		html := ui.RenderMeSubscriptions(state)
		assert.Contains(t, html, `id="me-subs-section"`)
		assert.NotContains(t, html, `id="me-subs-section" hidden`)
	})

	t.Run("list toggle button shows Show subscriptions when collapsed", func(t *testing.T) {
		t.Parallel()
		state := meSubsState(nil, 0, 1, 10)
		state.ListVisible = false
		html := ui.RenderMeSubscriptions(state)
		assert.Contains(t, html, `id="me-list-toggle"`)
		assert.Contains(t, html, "Show subscriptions")
		assert.NotContains(t, html, "Hide subscriptions")
	})

	t.Run("list toggle button shows Hide subscriptions when expanded", func(t *testing.T) {
		t.Parallel()
		state := meSubsState(nil, 0, 1, 10)
		state.ListVisible = true
		html := ui.RenderMeSubscriptions(state)
		assert.Contains(t, html, "Hide subscriptions")
		assert.NotContains(t, html, "Show subscriptions")
	})

	t.Run("chart slot renders skeleton when Loading and no series", func(t *testing.T) {
		t.Parallel()
		state := meSubsState([]dto.MeSubscriptionRow{singleItem()}, 1, 1, 10)
		state.Chart = application.OverlayChartState{
			Loading: true,
			Errors:  map[string]error{},
		}
		html := ui.RenderMeSubscriptions(state)
		assert.Contains(t, html, `class="overlay-chart-skeleton"`)
		assert.NotContains(t, html, `<polyline`)
	})

	t.Run("chart slot renders empty state when no subscriptions", func(t *testing.T) {
		t.Parallel()
		state := meSubsState(nil, 0, 1, 10)
		html := ui.RenderMeSubscriptions(state)
		assert.Contains(t, html, "No subscriptions to chart")
	})

	t.Run("chart slot renders chart-unavailable when all series errored", func(t *testing.T) {
		t.Parallel()
		state := meSubsState([]dto.MeSubscriptionRow{singleItem()}, 1, 1, 10)
		state.Chart = application.OverlayChartState{
			Loading: false,
			Errors:  map[string]error{"usd-eur": errors.New("timeout")},
		}
		html := ui.RenderMeSubscriptions(state)
		assert.Contains(t, html, `class="overlay-chart-error"`)
		assert.Contains(t, html, "Chart unavailable")
	})

	t.Run("chart slot renders overlay chart when Loaded with one series", func(t *testing.T) {
		t.Parallel()
		series := []application.SeriesData{
			{Name: "USD/EUR", Color: "#e53935", Points: []dto.ChartPointResponse{
				{Label: "2026-01-01", Price: 1.0},
				{Label: "2026-01-02", Price: 1.1},
			}},
		}
		state := stateWithSeries([]dto.MeSubscriptionRow{singleItem()}, series)
		html := ui.RenderMeSubscriptions(state)
		assert.Contains(t, html, `<polyline`)
		assert.Contains(t, html, `class="overlay-chart"`)
	})

	t.Run("chart slot renders overlay chart with partial errors", func(t *testing.T) {
		t.Parallel()
		series := []application.SeriesData{
			{Name: "USD/EUR", Color: "#e53935", Points: []dto.ChartPointResponse{
				{Label: "2026-01-01", Price: 1.0},
				{Label: "2026-01-02", Price: 1.1},
			}},
		}
		state := stateWithSeries([]dto.MeSubscriptionRow{singleItem()}, series)
		state.Chart.Errors = map[string]error{"gbp-usd": errors.New("timeout")}
		html := ui.RenderMeSubscriptions(state)
		// Partial render: one series succeeded so chart is rendered, not error state.
		assert.Contains(t, html, `<polyline`)
		assert.NotContains(t, html, "Chart unavailable")
	})

	t.Run("auth failure hides chart and toggle", func(t *testing.T) {
		t.Parallel()
		state := meSubsState(nil, 0, 1, 10)
		state.AuthFailure = true
		html := ui.RenderMeSubscriptions(state)
		assert.NotContains(t, html, `id="me-period-toggle"`)
		assert.NotContains(t, html, `id="me-overlay-chart"`)
		assert.NotContains(t, html, `id="me-list-toggle"`)
	})
}
