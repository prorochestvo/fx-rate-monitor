// Package application contains page controllers for the WASM frontend.
// This file implements the unauthenticated guest landing page.
package application

import (
	"context"

	"github.com/seilbekskindirov/monitor/cmd/wasm/apiclient"
	"github.com/seilbekskindirov/monitor/internal/dto"
)

// PublicChartDefaultLimit is the default page size used when loading the
// public sparkline-list chart.
const PublicChartDefaultLimit = 20

// PublicSubscriptionsState is a read-only snapshot the UI layer consumes to
// render the public (unauthenticated) sparkline list. No user-keyed data is
// present; the state is entirely derived from the server-side /api/public/...
// endpoint family.
type PublicSubscriptionsState struct {
	// Chart holds the paginated sparkline-list returned by /api/public/rates/chart.
	// Nil means the chart has not been fetched yet or the last fetch returned no data.
	Chart *dto.PublicChartResponse
	// ChartLoading is true while the chart fetch is in flight.
	ChartLoading bool
	// ChartError holds the most recent chart fetch error. Nil on success.
	ChartError error

	// OpenPair is the canonical pair label of the row whose detail modal is
	// open, or nil when no modal is visible.
	OpenPair *string

	// Page is the 1-based current page number (mirrors Chart.Page when non-nil).
	Page int
	// Limit is the page size in use.
	Limit int
	// Total is the unpaginated row count from the most recent fetch.
	Total int64
}

// PublicSubscriptionsPage is the page controller for the unauthenticated guest
// landing page. It is pure Go with no syscall/js dependencies and is therefore
// testable under the host toolchain via make test.
//
// Concurrency note: Go-WASM runs on a single OS thread, so state mutations
// within a single goroutine are safe without a mutex. If the project ever
// moves to multi-threaded WASM, add a sync.Mutex around state reads/writes.
type PublicSubscriptionsPage struct {
	client *apiclient.Client
	state  PublicSubscriptionsState
}

// NewPublicSubscriptionsPage constructs a controller backed by the given client.
func NewPublicSubscriptionsPage(client *apiclient.Client) *PublicSubscriptionsPage {
	return &PublicSubscriptionsPage{
		client: client,
		state: PublicSubscriptionsState{
			Limit: PublicChartDefaultLimit,
			Page:  1,
		},
	}
}

// State returns a snapshot of the current controller state. The caller must
// not mutate the returned value.
func (p *PublicSubscriptionsPage) State() PublicSubscriptionsState { return p.state }

// LoadPage fetches the given page (1-based) from /api/public/rates/chart and
// updates the controller state. ChartLoading is set to true before the fetch
// and reset to false when the fetch returns, regardless of outcome. ChartError
// is set on failure; Page, Total, and Chart are updated on success.
func (p *PublicSubscriptionsPage) LoadPage(ctx context.Context, page int) error {
	if page < 1 {
		page = 1
	}
	limit := p.state.Limit
	if limit < 1 {
		limit = PublicChartDefaultLimit
	}

	p.state.ChartLoading = true
	p.state.ChartError = nil

	resp, err := p.client.PublicRatesChart(ctx, page, limit)
	p.state.ChartLoading = false
	if err != nil {
		p.state.ChartError = err
		return err
	}
	p.state.Chart = &resp
	p.state.Page = page
	p.state.Total = resp.Total

	// Auto-clear the open modal when the refreshed chart no longer contains
	// the open pair (e.g. the pair was deactivated between page loads).
	if p.state.OpenPair != nil && !FindPairInPublicChart(&resp, *p.state.OpenPair) {
		p.ClosePairModal()
	}

	return nil
}

// OpenPairModal sets the open modal to the given canonical pair label.
// The pair value is copied so callers cannot mutate state through a shared
// backing pointer after the call returns.
func (p *PublicSubscriptionsPage) OpenPairModal(pair string) {
	cp := pair
	p.state.OpenPair = &cp
}

// ClosePairModal clears the open modal.
func (p *PublicSubscriptionsPage) ClosePairModal() {
	p.state.OpenPair = nil
}

// FindPairInPublicChart reports whether chart contains a row whose Pair field
// equals pair. Returns false when chart is nil.
func FindPairInPublicChart(chart *dto.PublicChartResponse, pair string) bool {
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
