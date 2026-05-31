package application

import (
	"context"
	"strings"

	"github.com/seilbekskindirov/monitor/cmd/wasm/apiclient"
	"github.com/seilbekskindirov/monitor/internal/dto"
)

// MeSubscriptionsBatchSize is the number of subscription rows fetched in a
// single call to /api/me/subscriptions. The full batch is held in memory so
// the modal can join all of the user's pairs against it; the list itself is
// no longer rendered in the UI.
const MeSubscriptionsBatchSize = 200

// MeHistoryDefaultLimit is the default page size used when loading per-pair
// rate history.
const MeHistoryDefaultLimit = 20

// AuthFailureSentinel is the error message produced by the apiclient when the
// server returns 401. The controller and the UI layer both match on this prefix
// to route to the auth-failure UX instead of the generic error UX.
const AuthFailureSentinel = "http 401"

// MeSubscriptionsState is a read-only snapshot the UI layer consumes to render
// one of three possible states: loading-skeleton, chart-list, or error
// (auth-failure or generic). The subscription list section has been removed;
// items are now used only for the per-pair modal condition badges.
type MeSubscriptionsState struct {
	Items []dto.MeSubscriptionRow
	// AuthFailure is true when the server responded 401 (initData HMAC failed).
	// The UI renders the "open from bot" message and hides the chart.
	AuthFailure bool
	// LastError holds the most recent non-nil error. Nil means the last fetch
	// succeeded (or LoadInitial has not been called yet).
	LastError error

	// OpenPair is the canonical pair label of the row whose detail modal is
	// open, or nil when no modal is visible.
	OpenPair *string

	// Chart holds the sparkline-list chart data returned by /api/me/rates/chart.
	// Nil means the chart has not been fetched yet or returned no data.
	Chart *dto.MeChartResponse
	// ChartLoading is true while the chart fetch is in flight.
	ChartLoading bool
	// ChartError holds the most recent chart fetch error. Nil on success.
	ChartError error

	// HistoryOpen is true when the modal body has swapped from the detail view
	// to the history view. Meaningless when OpenPair is nil.
	HistoryOpen bool
	// HistoryItems is the current page of history rows for OpenPair. Nil before
	// any load; never nil after a successful load even when empty.
	HistoryItems []dto.MeHistoryRow
	// HistoryPage is the 1-based page index currently displayed.
	HistoryPage int
	// HistoryLimit is the page size currently displayed. Persists across
	// pagination so prev / next reuse it.
	HistoryLimit int
	// HistoryTotal is the unpaginated total from the most recent fetch.
	HistoryTotal int64
	// HistoryLoading is true while a history fetch is in flight.
	HistoryLoading bool
	// HistoryError is the most recent non-nil history fetch error. Nil on success.
	HistoryError error
}

// MeSubscriptionsPage is the page controller for the Telegram Mini App
// subscriptions screen. It is pure Go with no syscall/js dependencies and
// is therefore testable under the host toolchain via make test.
//
// Concurrency note: Go-WASM runs on a single OS thread, so state mutations
// within a single goroutine are safe without a mutex. If the project ever
// moves to multi-threaded WASM, add a sync.Mutex around state reads/writes.
type MeSubscriptionsPage struct {
	client   *apiclient.Client
	initData string
	pageSize int
	state    MeSubscriptionsState
}

// NewMeSubscriptionsPage constructs a controller. initData is the Telegram
// WebApp initData string read once at WASM boot from window.Telegram.WebApp;
// it is forwarded unchanged on every MeSubscriptions call.
// pageSize controls how many rows the server is asked for per request.
func NewMeSubscriptionsPage(client *apiclient.Client, initData string, pageSize int) *MeSubscriptionsPage {
	if pageSize <= 0 {
		pageSize = MeSubscriptionsBatchSize
	}
	return &MeSubscriptionsPage{
		client:   client,
		initData: initData,
		pageSize: pageSize,
	}
}

// State returns a snapshot of the current controller state. The caller must
// not mutate the returned slice.
func (p *MeSubscriptionsPage) State() MeSubscriptionsState { return p.state }

// LoadInitial fetches the first batch of subscriptions. It is called once at
// screen mount before any user interaction.
func (p *MeSubscriptionsPage) LoadInitial(ctx context.Context) error {
	return p.fetchAndStore(ctx)
}

// OpenPairModal sets the open modal to the given canonical pair label.
// The pair value is copied so that callers cannot mutate state through a shared
// backing pointer after the call returns.
func (p *MeSubscriptionsPage) OpenPairModal(pair string) {
	cp := pair
	p.state.OpenPair = &cp
}

// ClosePairModal clears the open modal and resets all history state except
// HistoryLimit, which persists so a re-open uses the same page size.
func (p *MeSubscriptionsPage) ClosePairModal() {
	p.state.OpenPair = nil
	p.state.HistoryOpen = false
	p.state.HistoryItems = nil
	p.state.HistoryPage = 0
	p.state.HistoryTotal = 0
	p.state.HistoryError = nil
	// HistoryLoading is left; it will be cleared by the inflight goroutine.
}

// OpenHistory switches the modal body to the history view for the currently
// open pair and triggers a load of page 1. No-op when OpenPair is nil.
func (p *MeSubscriptionsPage) OpenHistory(ctx context.Context) error {
	if p.state.OpenPair == nil {
		return nil
	}
	p.state.HistoryOpen = true
	return p.LoadHistory(ctx, 1)
}

// CloseHistory switches the modal body back to the detail view. The current
// page of history items is preserved in state so re-opening is instant;
// callers must call LoadHistory explicitly if they want a refetch.
func (p *MeSubscriptionsPage) CloseHistory() {
	p.state.HistoryOpen = false
}

// LoadHistory fetches one page of history rows for the currently open pair.
// page is 1-based. The state's HistoryPage, HistoryItems, and HistoryTotal are
// updated on success; HistoryError is set on failure. No-op (returns nil) when
// OpenPair is nil.
//
// HistoryLoading is set to true synchronously before the fetch and reset to
// false when the fetch returns, regardless of outcome.
//
// The target pair is snapshotted before the HTTP call. If the modal is closed
// or switched to a different pair while the fetch is in flight, the stale
// result is silently dropped — state is not overwritten with data for the
// wrong pair.
//
// A 401 response sets AuthFailure=true and resets the modal to a clean state
// so the next mount starts fresh.
func (p *MeSubscriptionsPage) LoadHistory(ctx context.Context, page int) error {
	if p.state.OpenPair == nil {
		return nil
	}
	if p.state.HistoryLimit <= 0 {
		p.state.HistoryLimit = MeHistoryDefaultLimit
	}
	targetPair := *p.state.OpenPair
	p.state.HistoryLoading = true
	defer func() { p.state.HistoryLoading = false }()

	resp, err := p.client.MeRatesHistory(ctx, p.initData, targetPair, page, p.state.HistoryLimit)
	if err != nil {
		if strings.Contains(err.Error(), AuthFailureSentinel) {
			p.state.AuthFailure = true
			p.state.OpenPair = nil
			p.state.HistoryOpen = false
			p.state.HistoryItems = nil
			p.state.HistoryPage = 0
			p.state.HistoryTotal = 0
		}
		p.state.HistoryError = err
		return err
	}
	// Discard stale result when the modal was closed or switched mid-fetch.
	if p.state.OpenPair == nil || *p.state.OpenPair != targetPair {
		return nil
	}
	p.state.HistoryError = nil
	p.state.HistoryPage = page
	p.state.HistoryItems = resp.Items
	p.state.HistoryTotal = resp.Total
	return nil
}

// HistoryNextPage loads the next page when more rows exist. No-op at the end
// of the result set (when HistoryPage * HistoryLimit >= HistoryTotal).
func (p *MeSubscriptionsPage) HistoryNextPage(ctx context.Context) error {
	limit := p.state.HistoryLimit
	if limit <= 0 {
		limit = MeHistoryDefaultLimit
	}
	if int64(p.state.HistoryPage*limit) >= p.state.HistoryTotal {
		return nil
	}
	return p.LoadHistory(ctx, p.state.HistoryPage+1)
}

// HistoryPrevPage loads the previous page. No-op when already on page 1.
func (p *MeSubscriptionsPage) HistoryPrevPage(ctx context.Context) error {
	if p.state.HistoryPage <= 1 {
		return nil
	}
	return p.LoadHistory(ctx, p.state.HistoryPage-1)
}

// LoadSparklineChart fetches the sparkline-list chart from /api/me/rates/chart
// and stores the result in state. ChartLoading is set to true before the fetch
// and false after it completes (success or error). ChartError is set on failure.
// After a successful fetch, if OpenPair is set but the new chart no longer
// contains a matching pair, OpenPair is cleared so the modal slot stays honest.
// The caller is responsible for re-rendering after this call returns.
func (p *MeSubscriptionsPage) LoadSparklineChart(ctx context.Context) error {
	p.state.ChartLoading = true
	p.state.ChartError = nil

	resp, err := p.client.MeRatesChart(ctx, p.initData)
	p.state.ChartLoading = false
	if err != nil {
		p.state.ChartError = err
		return err
	}
	p.state.Chart = &resp

	// Auto-clear the open modal (and history state) when the refreshed chart no
	// longer contains the pair it was showing. This prevents a stale modal.
	if p.state.OpenPair != nil && !FindPairInChart(&resp, *p.state.OpenPair) {
		p.ClosePairModal()
	}

	return nil
}

// FindPairInChart reports whether chart contains a row whose Pair field equals pair.
// Returns false when chart is nil.
func FindPairInChart(chart *dto.MeChartResponse, pair string) bool {
	if chart == nil {
		return false
	}
	for _, row := range chart.Pairs {
		if row.Pair == pair {
			return true
		}
	}
	return false
}

// fetchAndStore calls the API client and stores the result in state. It
// returns the error (also stored in state for UI inspection). A 401 error sets
// AuthFailure=true so the UI can show the "open from bot" message.
func (p *MeSubscriptionsPage) fetchAndStore(ctx context.Context) error {
	resp, err := p.client.MeSubscriptions(ctx, p.initData, 1, p.pageSize, "")
	if err != nil {
		p.state.Items = nil
		p.state.AuthFailure = strings.Contains(err.Error(), AuthFailureSentinel)
		p.state.LastError = err
		return err
	}
	p.state.Items = resp.Items
	p.state.AuthFailure = false
	p.state.LastError = nil
	return nil
}
