package application

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/seilbekskindirov/monitor/cmd/wasm/apiclient"
	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/dto"
)

// Period constants for the overlay chart toggle.
const (
	MeSubscriptionsPeriodWeek    = "week"
	MeSubscriptionsPeriodMonth   = "month"
	MeSubscriptionsPeriodYear    = "year"
	MeSubscriptionsDefaultPeriod = MeSubscriptionsPeriodWeek
)

// MeSubscriptionsPageSize is the default page size sent to /api/me/subscriptions.
const MeSubscriptionsPageSize = 10

// authFailureSentinel is the error message produced by the apiclient when the
// server returns 401. The controller matches on this prefix to route to the
// auth-failure UX instead of the generic error UX.
const authFailureSentinel = "http 401"

// meSubsChartPalette is the 8-color cycle for overlay chart series.
// Positional, not hash-based — see Trade-off 2 in plan 009 / Risk 4.
// Series are sorted by source name; the n-th in sort order takes
// meSubsChartPalette[n % len(meSubsChartPalette)]. Do not "optimize"
// this to a hash: the positional scheme guarantees zero collisions at N≤8
// while hash-based assignment has birthday-paradox collisions at small N.
var meSubsChartPalette = []string{
	"#e53935", // red
	"#1e88e5", // blue
	"#43a047", // green
	"#fb8c00", // orange
	"#8e24aa", // purple
	"#00acc1", // cyan
	"#fdd835", // yellow
	"#6d4c41", // brown
}

// ErrStaleGeneration is returned by LoadChart when the generation token passed
// by the caller does not match the current generation counter. The caller
// (main.go fanout) must errors.Is-check this and skip any DOM update.
var ErrStaleGeneration = errors.New("stale generation")

// SeriesData is the transport representation of one chart series produced by
// the application layer. The UI layer maps it to ui.Series to avoid an
// application → ui import cycle (ui already imports application).
type SeriesData struct {
	Name   string
	Color  string
	Points []dto.ChartPointResponse
}

// OverlayChartState holds the current state of the multi-series overlay chart.
// Errors is keyed by source name; a per-source fetch failure does not poison
// the whole chart — partial rendering is acceptable.
type OverlayChartState struct {
	Loading bool
	Loaded  bool
	Series  []SeriesData
	Errors  map[string]error
}

// MeSubscriptionsState is a read-only snapshot the UI layer consumes to render
// one of four possible states: loading-skeleton, card-list, empty-list, or
// error (auth-failure or generic).
type MeSubscriptionsState struct {
	Items    []dto.MeSubscriptionRow
	Total    int64
	Page     int
	PageSize int
	Query    string
	// AuthFailure is true when the server responded 401 (initData HMAC failed).
	// The UI renders the "open from bot" message and hides pagination.
	AuthFailure bool
	// LastError holds the most recent non-nil error. Nil means the last fetch
	// succeeded (or LoadInitial has not been called yet).
	LastError   error
	Period      string
	Chart       OverlayChartState
	ListVisible bool
}

// MeSubscriptionsPage is the page controller for the Telegram Mini App
// subscriptions screen. It is pure Go with no syscall/js dependencies and
// is therefore testable under the host toolchain via make test.
//
// Concurrency note: Go-WASM runs on a single OS thread, so state mutations
// within a single goroutine are safe without a mutex. The debounce timer fires
// its callback on a new goroutine, but that goroutine only writes state and
// calls the onResult callback — it never reads from another goroutine
// concurrently. If the project ever moves to multi-threaded WASM, add a
// sync.Mutex around state reads/writes.
//
// The gen field is atomic.Int64 so that application-layer tests run under
// go test -race (which uses real goroutines) without tripping the detector.
// WASM itself is single-threaded, but the host test toolchain is not.
type MeSubscriptionsPage struct {
	client   *apiclient.Client
	initData string
	state    MeSubscriptionsState

	// gen is the monotonic generation counter. Incremented by every
	// state-resetting operation (BeginChartLoad, SetPeriod).
	// atomic.Int64 zero value is valid; no constructor call needed.
	gen atomic.Int64

	// chartExpected is the number of series LoadChart calls expected in the
	// current fanout. Set by BeginChartLoad; used to clear Loading when all
	// goroutines have settled.
	chartExpected int

	// debounce holds the pending search timer. Stop and reset on every
	// OnSearch call so only the final keystroke triggers a fetch.
	debounce *time.Timer
}

// NewMeSubscriptionsPage constructs a controller. initData is the Telegram
// WebApp initData string read once at WASM boot from window.Telegram.WebApp;
// it is forwarded unchanged on every MeSubscriptions call.
// pageSize controls how many rows the server is asked for per request.
func NewMeSubscriptionsPage(client *apiclient.Client, initData string, pageSize int) *MeSubscriptionsPage {
	if pageSize <= 0 {
		pageSize = MeSubscriptionsPageSize
	}
	return &MeSubscriptionsPage{
		client:   client,
		initData: initData,
		state: MeSubscriptionsState{
			Page:     1,
			PageSize: pageSize,
			Period:   MeSubscriptionsDefaultPeriod,
			Chart:    OverlayChartState{Errors: map[string]error{}},
		},
	}
}

// State returns a snapshot of the current controller state. The caller must
// not mutate the returned slice.
func (p *MeSubscriptionsPage) State() MeSubscriptionsState { return p.state }

// SnapshotGeneration returns the current generation counter value. Callers
// capture this before launching LoadChart goroutines and pass it back so stale
// results can be detected.
func (p *MeSubscriptionsPage) SnapshotGeneration() int64 { return p.gen.Load() }

// LoadInitial fetches the first page of subscriptions. It is called once at
// screen mount before any user interaction.
func (p *MeSubscriptionsPage) LoadInitial(ctx context.Context) error {
	p.state.Page = 1
	return p.fetchAndStore(ctx)
}

// NextPage increments the page counter and fetches the next page.
// It mirrors the JS "next" button handler: there is no upper-bound guard
// in the controller — the caller is responsible for not offering the Next
// button when the current page is already the last one (i.e. when
// len(Items) < PageSize or via Total math).
func (p *MeSubscriptionsPage) NextPage(ctx context.Context) error {
	p.state.Page++
	return p.fetchAndStore(ctx)
}

// PrevPage decrements the page counter and fetches the previous page.
// It mirrors the JS "prev" button handler: page is not decremented below 1.
func (p *MeSubscriptionsPage) PrevPage(ctx context.Context) error {
	if p.state.Page <= 1 {
		return nil
	}
	p.state.Page--
	return p.fetchAndStore(ctx)
}

// OnSearch stores the new query, resets to page 1, and schedules a fetch
// 250 ms after the last call. If a previous timer is still pending it is
// cancelled so only the final keystroke fires a network request.
//
// The returned channel receives the fetch error (nil on success) exactly once,
// after the debounced fetch has settled. The caller (cmd/wasm/main.go) listens
// on this channel to know when to re-render the section and to log any error.
//
// Design choice: channel over callback. A channel lets the caller select{}
// it alongside other signals (e.g. context cancellation) without the
// controller needing to know about the DOM. Each OnSearch call returns a
// fresh channel; the caller should discard the channel from the previous
// call once it starts listening on the new one.
func (p *MeSubscriptionsPage) OnSearch(q string) <-chan error {
	p.state.Query = q
	p.state.Page = 1

	done := make(chan error, 1)

	if p.debounce != nil {
		p.debounce.Stop()
	}
	p.debounce = time.AfterFunc(250*time.Millisecond, func() {
		done <- p.fetchAndStore(context.Background())
	})

	return done
}

// SetPeriod updates the chart period. Returns a PublicError ("Invalid period.")
// when period is not one of the three accepted values; state is not mutated
// on the invalid path. When the period changes, the chart series and errors
// are cleared and the generation counter is bumped so any in-flight LoadChart
// goroutines from the previous period drop their results on return.
func (p *MeSubscriptionsPage) SetPeriod(_ context.Context, period string) error {
	switch period {
	case MeSubscriptionsPeriodWeek, MeSubscriptionsPeriodMonth, MeSubscriptionsPeriodYear:
	default:
		return internal.NewPublicError("Invalid period.")
	}
	if p.state.Period == period {
		return nil
	}
	p.state.Period = period
	p.state.Chart = OverlayChartState{Loading: true, Errors: map[string]error{}}
	p.gen.Add(1)
	return nil
}

// BeginChartLoad increments the generation counter, resets the chart state to
// a fresh loading state, stores the expected number of LoadChart calls, and
// returns the new generation token. Callers pass this token to each LoadChart
// goroutine so stale results from prior fanouts are discarded.
//
// Why a separate BeginChartLoad rather than folding into SetPeriod: the fanout
// also happens after LoadInitial and NextPage/PrevPage, not only on period
// change. Centralizing the "increment-gen-and-reset-chart" sequence avoids
// duplicating it at every call site.
//
// The returned generation is the value all subsequent LoadChart calls in this
// fanout must pass. Any LoadChart with a different generation is a no-op.
func (p *MeSubscriptionsPage) BeginChartLoad(expected int) int64 {
	p.gen.Add(1)
	p.state.Chart = OverlayChartState{
		Loading: expected > 0,
		Errors:  map[string]error{},
	}
	p.chartExpected = expected
	return p.gen.Load()
}

// LoadChart fetches chart data for one source and stores the result. The gen
// parameter is the generation token captured at fanout time (from BeginChartLoad).
//
// Stale-fetch guard: if gen does not match the current generation at entry or
// after the fetch returns, the result is silently dropped and ErrStaleGeneration
// is returned. The caller (main.go) must errors.Is-check this sentinel and skip
// any DOM update.
//
// On success the series list is re-sorted by source name and colors are
// re-assigned positionally so the legend order is always deterministic. On
// error the per-source error is stored in Chart.Errors.
func (p *MeSubscriptionsPage) LoadChart(ctx context.Context, sourceName string, gen int64) error {
	if gen != p.gen.Load() {
		return ErrStaleGeneration
	}

	points, err := p.client.RatesChart(ctx, sourceName, p.state.Period)

	// Recheck after the fetch — another operation may have bumped the generation.
	if gen != p.gen.Load() {
		return ErrStaleGeneration
	}

	if err != nil {
		p.state.Chart.Errors[sourceName] = err
		p.maybeFinishLoading()
		return err
	}

	// Find the display name for this source.
	displayName := sourceName
	for _, item := range p.state.Items {
		if item.SourceName == sourceName {
			if item.SourceTitle != "" {
				displayName = item.SourceTitle
			}
			break
		}
	}

	// Upsert: replace existing entry for this source if present, else append.
	found := false
	for i, s := range p.state.Chart.Series {
		if s.Name == displayName || s.Name == sourceName {
			p.state.Chart.Series[i].Points = points
			found = true
			break
		}
	}
	if !found {
		p.state.Chart.Series = append(p.state.Chart.Series, SeriesData{
			Name:   displayName,
			Points: points,
		})
	}

	// Re-sort by source name for stable legend order.
	sort.Slice(p.state.Chart.Series, func(i, j int) bool {
		return p.state.Chart.Series[i].Name < p.state.Chart.Series[j].Name
	})

	// Re-assign colors positionally after sort. This is the positional palette
	// assignment: n-th series in alphabetical order takes palette[n % len].
	for i := range p.state.Chart.Series {
		p.state.Chart.Series[i].Color = meSubsChartPalette[i%len(meSubsChartPalette)]
	}

	p.state.Chart.Loaded = true
	p.maybeFinishLoading()
	return nil
}

// ToggleListVisible flips the ListVisible flag and returns the new value.
func (p *MeSubscriptionsPage) ToggleListVisible() bool {
	p.state.ListVisible = !p.state.ListVisible
	return p.state.ListVisible
}

// fetchAndStore calls the API client, stores the result in state, calls
// BeginChartLoad so the chart fans out for the newly loaded items, and returns
// the error (also stored in state for UI inspection). A 401 error sets
// AuthFailure=true so the UI can show the "open from bot" message.
//
// BeginChartLoad is called with len(items) after a successful fetch. Main.go
// reads the generation after fetchAndStore returns and launches the goroutines.
// On error, BeginChartLoad is not called — there are no items to chart.
func (p *MeSubscriptionsPage) fetchAndStore(ctx context.Context) error {
	resp, err := p.client.MeSubscriptions(ctx, p.initData, p.state.Page, p.state.PageSize, p.state.Query)
	if err != nil {
		p.state.Items = nil
		p.state.Total = 0
		p.state.AuthFailure = strings.Contains(err.Error(), authFailureSentinel)
		p.state.LastError = err
		return err
	}
	p.state.Items = resp.Items
	p.state.Total = resp.Total
	p.state.AuthFailure = false
	p.state.LastError = nil
	// Prepare chart state for the new items. The caller (main.go) launches
	// the goroutines after fetchAndStore returns.
	p.BeginChartLoad(len(resp.Items))
	return nil
}

// maybeFinishLoading clears Chart.Loading when all expected series have
// settled (resolved or errored). Called after every LoadChart result.
func (p *MeSubscriptionsPage) maybeFinishLoading() {
	settled := len(p.state.Chart.Series) + len(p.state.Chart.Errors)
	if settled >= p.chartExpected {
		p.state.Chart.Loading = false
	}
}
