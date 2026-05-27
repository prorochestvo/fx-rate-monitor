package ui

import (
	"fmt"
	"strings"

	"github.com/seilbekskindirov/monitor/cmd/wasm/application"
	"github.com/seilbekskindirov/monitor/cmd/wasm/dom"
	"github.com/seilbekskindirov/monitor/internal/dto"
)

// authFailureMsg is the exact copy from subscriptions.html lines 85 and 121.
// Both the empty-initData path and the 401-response path render this message.
const authFailureMsg = "This page must be opened from the bot&#39;s button. Please reopen via the bot."

// defaultOverlayOpts is the viewBox used for the overlay chart in the Mini App.
var defaultOverlayOpts = OverlayOptions{Width: 320, Height: 180}

// RenderMeSubscriptions returns the full HTML for the Mini App subscriptions
// screen. Default top-to-bottom layout:
//  1. Period toggle bar (week / month / year).
//  2. Overlay chart slot (skeleton, empty-state, error, or rendered chart).
//  3. "Show/Hide subscriptions" toggle button.
//  4. Subscription list section — hidden when state.ListVisible is false.
//
// AuthFailure short-circuits the whole screen to just the auth-failure message.
//
// Every user-influenced field is passed through dom.Escape before interpolation.
func RenderMeSubscriptions(state application.MeSubscriptionsState) string {
	if state.AuthFailure {
		return fmt.Sprintf(`<p class="error-msg">%s</p>`, authFailureMsg)
	}

	var b strings.Builder
	b.WriteString(renderPeriodToggle(state.Period))
	b.WriteString(`<div id="me-overlay-chart">`)
	b.WriteString(renderOverlayChartSlot(state))
	b.WriteString(`</div>`)
	b.WriteString(renderListToggleButton(state.ListVisible))
	b.WriteString(renderListSection(state))
	return b.String()
}

// RenderMeSubsList returns only the inner content HTML for the list section so
// the DOM can be updated in-place without re-rendering the chart or period toggle
// (avoids losing input focus).
func RenderMeSubsList(state application.MeSubscriptionsState) string {
	return renderMeSubsContent(state)
}

// RenderMeSubsPagination returns only the pagination HTML. Returns empty string
// when the state signals an auth-failure or a generic error, because pagination
// must not be shown in either error case.
func RenderMeSubsPagination(state application.MeSubscriptionsState) string {
	if state.AuthFailure || state.LastError != nil {
		return ""
	}
	return renderMeSubsPagination(state)
}

// renderPeriodToggle returns the period-toggle button bar. The active period
// gets the "active" modifier class. Button labels are display-capitalized
// English; the data-period attribute carries the lowercase value forwarded
// to MeSubscriptionsPage.SetPeriod.
func renderPeriodToggle(currentPeriod string) string {
	type btn struct{ data, label string }
	buttons := []btn{
		{application.MeSubscriptionsPeriodWeek, "Week"},
		{application.MeSubscriptionsPeriodMonth, "Month"},
		{application.MeSubscriptionsPeriodYear, "Year"},
	}
	var b strings.Builder
	b.WriteString(`<div class="period-toggle" id="me-period-toggle">`)
	for _, x := range buttons {
		cls := "period-btn"
		if x.data == currentPeriod {
			cls = "period-btn active"
		}
		fmt.Fprintf(&b, `<button class="%s" data-period="%s">%s</button>`,
			cls, x.data, x.label)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// RenderOverlayChartSlot returns the content for the #me-overlay-chart div.
// Exported so main.go can update the chart slot in-place without re-rendering
// the whole page (which would blow away search input focus).
func RenderOverlayChartSlot(state application.MeSubscriptionsState) string {
	return renderOverlayChartSlot(state)
}

// renderOverlayChartSlot returns the content for the #me-overlay-chart div.
func renderOverlayChartSlot(state application.MeSubscriptionsState) string {
	ch := state.Chart

	// All fetches errored and nothing resolved successfully → chart unavailable.
	if len(ch.Errors) > 0 && len(ch.Series) == 0 && !ch.Loading {
		return `<p class="overlay-chart-error">Chart unavailable</p>`
	}

	// No subscriptions at all — nothing to chart.
	if len(state.Items) == 0 && !ch.Loading {
		return `<p class="overlay-chart-empty">No subscriptions to chart. Subscribe to a source first.</p>`
	}

	// Loading with no series yet → skeleton.
	if ch.Loading && len(ch.Series) == 0 {
		return `<div class="overlay-chart-skeleton"></div>`
	}

	// At least one series resolved (possibly with some errors too) → render chart.
	uiSeries := toUISeries(ch.Series)
	return RenderOverlayChart(uiSeries, defaultOverlayOpts)
}

// toUISeries converts application SeriesData to ui.Series for rendering.
// This mapping exists to avoid an application → ui import cycle.
func toUISeries(data []application.SeriesData) []Series {
	out := make([]Series, len(data))
	for i, d := range data {
		out[i] = Series{
			Name:   d.Name,
			Color:  d.Color,
			Points: d.Points,
		}
	}
	return out
}

// renderListToggleButton returns the "Show/Hide subscriptions" toggle button.
func renderListToggleButton(visible bool) string {
	label := "Show subscriptions"
	if visible {
		label = "Hide subscriptions"
	}
	return fmt.Sprintf(
		`<div class="list-toggle-wrap"><button class="list-toggle" id="me-list-toggle">%s</button></div>`,
		label,
	)
}

// renderListSection returns the subscription list section. The section carries
// the HTML hidden attribute when ListVisible is false; removing the attribute
// (not setting it to "false") reveals it.
func renderListSection(state application.MeSubscriptionsState) string {
	hidden := ""
	if !state.ListVisible {
		hidden = " hidden"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div id="me-subs-section"%s>`, hidden)
	b.WriteString(renderSearchBar(state.Query))
	b.WriteString(`<div id="me-subs-list">`)
	b.WriteString(renderMeSubsContent(state))
	b.WriteString(`</div>`)
	b.WriteString(`<div id="me-subs-pagination">`)
	if !state.AuthFailure && state.LastError == nil {
		b.WriteString(renderMeSubsPagination(state))
	}
	b.WriteString(`</div>`)
	b.WriteString(`</div>`)
	return b.String()
}

func renderSearchBar(currentQuery string) string {
	return fmt.Sprintf(
		`<input class="search-bar" id="me-search" type="text" placeholder="Search subscriptions..." value="%s">`,
		dom.Escape(currentQuery),
	)
}

func renderMeSubsContent(state application.MeSubscriptionsState) string {
	if state.AuthFailure {
		return fmt.Sprintf(`<p class="error-msg">%s</p>`, authFailureMsg)
	}
	if state.LastError != nil {
		return fmt.Sprintf(
			`<p class="error-msg">Error loading subscriptions: %s</p>`,
			dom.Escape(state.LastError.Error()),
		)
	}
	if len(state.Items) == 0 {
		return `<p class="status">No subscriptions found.</p>`
	}
	var b strings.Builder
	for _, item := range state.Items {
		b.WriteString(renderMeSubCard(item))
	}
	return b.String()
}

func renderMeSubCard(item dto.MeSubscriptionRow) string {
	title := item.SourceTitle
	if title == "" {
		title = item.SourceName
	}

	var price string
	if item.LatestPrice != 0 {
		price = fmt.Sprintf("%.4f", item.LatestPrice)
	} else {
		price = "—"
	}

	ts := fmtDate(item.LatestAt)

	var b strings.Builder
	b.WriteString(`<div class="card">`)
	b.WriteString(fmt.Sprintf(`<div class="card-title">%s</div>`, dom.Escape(title)))

	if item.BaseCurrency != "" && item.QuoteCurrency != "" {
		pair := item.BaseCurrency + "/" + item.QuoteCurrency
		b.WriteString(fmt.Sprintf(`<div class="card-pair">%s</div>`, dom.Escape(pair)))
	}

	b.WriteString(fmt.Sprintf(`<div class="card-price">%s</div>`, dom.Escape(price)))
	b.WriteString(fmt.Sprintf(`<div class="card-time">Last grab: %s</div>`, dom.Escape(ts)))

	if len(item.Conditions) > 0 {
		b.WriteString(`<div class="badges">`)
		for _, c := range item.Conditions {
			b.WriteString(fmt.Sprintf(`<span class="badge">%s</span>`, dom.Escape(c)))
		}
		b.WriteString(`</div>`)
	}

	b.WriteString(`</div>`)
	return b.String()
}

func renderMeSubsPagination(state application.MeSubscriptionsState) string {
	ps := PaginationState{
		Page:    state.Page,
		Count:   len(state.Items),
		Limit:   state.PageSize,
		Section: "me-subs",
	}
	return RenderPagination(ps)
}
