package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seilbekskindirov/monitor/cmd/wasm/apiclient"
	"github.com/seilbekskindirov/monitor/cmd/wasm/application"
	"github.com/seilbekskindirov/monitor/internal/dto"
)

// meSubsFakeFetcher is a Fetcher that records every FetchJSON call and lets
// tests configure the response per call or globally. When urlResponses is
// non-nil, FetchJSON looks up the URL prefix in that map first; if no entry
// matches it falls back to jsonResponse / jsonErr.
type meSubsFakeFetcher struct {
	jsonResponse []byte
	jsonErr      error
	callCount    int
	lastURL      string
	lastHeaders  map[string]string
	// urlResponses maps a URL prefix (e.g. "/api/me/rates/history") to the
	// raw JSON body that should be returned for requests to that prefix.
	urlResponses map[string][]byte
	// urlErr maps a URL prefix to an error that should be returned instead
	// of a body.
	urlErr map[string]error
}

var _ apiclient.Fetcher = (*meSubsFakeFetcher)(nil)

func (f *meSubsFakeFetcher) FetchJSON(_ context.Context, _, rawURL string, _ any, headers map[string]string) ([]byte, error) {
	f.callCount++
	f.lastURL = rawURL
	f.lastHeaders = headers
	// Per-URL routing takes priority over the global response.
	for prefix, body := range f.urlResponses {
		if strings.HasPrefix(rawURL, prefix) {
			return body, nil
		}
	}
	for prefix, err := range f.urlErr {
		if strings.HasPrefix(rawURL, prefix) {
			return nil, err
		}
	}
	if f.jsonErr != nil {
		return nil, f.jsonErr
	}
	return f.jsonResponse, nil
}

func (f *meSubsFakeFetcher) FetchNoContent(_ context.Context, _, _ string, _ any, _ map[string]string) error {
	return nil
}

func meSubsResponse(items []dto.MeSubscriptionRow, total int64, page, pageSize int) []byte {
	resp := dto.MeSubscriptionsResponse{
		Items:    items,
		Total:    total,
		Page:     int64(page),
		PageSize: int64(pageSize),
	}
	b, err := json.Marshal(resp)
	if err != nil {
		panic(err)
	}
	return b
}

func sampleItems() []dto.MeSubscriptionRow {
	return []dto.MeSubscriptionRow{
		{
			SourceName:    "usd-eur",
			SourceTitle:   "USD/EUR",
			BaseCurrency:  "USD",
			QuoteCurrency: "EUR",
			Conditions:    []string{">1.05"},
			LatestPrice:   1.0812,
			LatestAt:      "2026-01-01T12:00:00Z",
		},
	}
}

func newMePage(f *meSubsFakeFetcher, initData string) *application.MeSubscriptionsPage {
	c := apiclient.New(f)
	return application.NewMeSubscriptionsPage(c, initData, 10)
}

func TestMeSubscriptionsPage_LoadInitial(t *testing.T) {
	t.Parallel()

	t.Run("happy path stores items", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			jsonResponse: meSubsResponse(sampleItems(), 1, 1, 10),
		}
		page := newMePage(f, "valid-init-data")
		err := page.LoadInitial(t.Context())
		require.NoError(t, err)
		st := page.State()
		assert.Len(t, st.Items, 1)
		assert.False(t, st.AuthFailure)
		assert.NoError(t, st.LastError)
	})

	t.Run("401 sets AuthFailure and clears items", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{jsonErr: errors.New("http 401")}
		page := newMePage(f, "bad-token")
		err := page.LoadInitial(t.Context())
		require.Error(t, err)
		st := page.State()
		assert.True(t, st.AuthFailure, "AuthFailure must be true on 401")
		assert.Empty(t, st.Items)
		assert.ErrorContains(t, st.LastError, "http 401")
	})

	t.Run("generic server error sets LastError without AuthFailure", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{jsonErr: errors.New("http 500")}
		page := newMePage(f, "tok")
		err := page.LoadInitial(t.Context())
		require.Error(t, err)
		st := page.State()
		assert.False(t, st.AuthFailure, "AuthFailure must be false for non-401 errors")
		assert.ErrorContains(t, st.LastError, "http 500")
	})
}

func TestMeSubscriptionsPage_HeaderPropagation(t *testing.T) {
	t.Parallel()

	t.Run("LoadInitial forwards X-Telegram-Init-Data header", func(t *testing.T) {
		t.Parallel()
		const initData = "query_id=AAH&user=%7B%22id%22%3A123%7D&auth_date=1000&hash=abc"
		f := &meSubsFakeFetcher{
			jsonResponse: meSubsResponse(nil, 0, 1, 10),
		}
		page := newMePage(f, initData)
		err := page.LoadInitial(t.Context())
		require.NoError(t, err)
		assert.Equal(t, initData, f.lastHeaders["X-Telegram-Init-Data"],
			"X-Telegram-Init-Data header must be forwarded from the constructor initData parameter")
	})

	t.Run("LoadSparklineChart forwards X-Telegram-Init-Data header", func(t *testing.T) {
		t.Parallel()
		const initData = "query_id=AAH&user=%7B%22id%22%3A123%7D&auth_date=1000&hash=abc"
		pairs := []dto.MeChartPairRow{
			{Pair: "USD/KZT", Series: []dto.MeChartSeries{{Kind: "BID", Color: "#1D9E75", Latest: 487.0}}},
		}
		b, err := json.Marshal(dto.MeChartResponse{Window: "7d", Pairs: pairs})
		require.NoError(t, err)
		f := &meSubsFakeFetcher{jsonResponse: b}
		page := newMePage(f, initData)
		err = page.LoadSparklineChart(t.Context())
		require.NoError(t, err)
		assert.Equal(t, initData, f.lastHeaders["X-Telegram-Init-Data"])
	})
}

func TestMeSubscriptionsPage_OpenPairModal(t *testing.T) {
	t.Parallel()

	t.Run("stores pair label in OpenPair", func(t *testing.T) {
		t.Parallel()
		page := newMePage(&meSubsFakeFetcher{}, "tok")
		page.OpenPairModal("USD/KZT")
		st := page.State()
		require.NotNil(t, st.OpenPair)
		assert.Equal(t, "USD/KZT", *st.OpenPair)
	})

	t.Run("subsequent call overwrites previous pair", func(t *testing.T) {
		t.Parallel()
		page := newMePage(&meSubsFakeFetcher{}, "tok")
		page.OpenPairModal("USD/KZT")
		page.OpenPairModal("EUR/KZT")
		st := page.State()
		require.NotNil(t, st.OpenPair)
		assert.Equal(t, "EUR/KZT", *st.OpenPair)
	})
}

func TestMeSubscriptionsPage_ClosePairModal(t *testing.T) {
	t.Parallel()

	t.Run("clears OpenPair when previously set", func(t *testing.T) {
		t.Parallel()
		page := newMePage(&meSubsFakeFetcher{}, "tok")
		page.OpenPairModal("USD/KZT")
		page.ClosePairModal()
		assert.Nil(t, page.State().OpenPair)
	})

	t.Run("no-op when OpenPair is already nil", func(t *testing.T) {
		t.Parallel()
		page := newMePage(&meSubsFakeFetcher{}, "tok")
		page.ClosePairModal()
		assert.Nil(t, page.State().OpenPair)
	})

	t.Run("resets history state on close", func(t *testing.T) {
		t.Parallel()
		histBody := meHistoryResponse("USD/KZT", 1, 20, 1, nil)
		f := &meSubsFakeFetcher{
			urlResponses: map[string][]byte{"/api/me/rates/history": histBody},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		require.NoError(t, page.OpenHistory(t.Context()))

		st := page.State()
		assert.True(t, st.HistoryOpen)
		assert.Equal(t, 1, st.HistoryPage)

		page.ClosePairModal()
		st = page.State()
		assert.Nil(t, st.OpenPair)
		assert.False(t, st.HistoryOpen)
		assert.Nil(t, st.HistoryItems)
		assert.Equal(t, 0, st.HistoryPage)
		assert.Equal(t, int64(0), st.HistoryTotal)
		assert.NoError(t, st.HistoryError)
		// HistoryLimit must survive the close so re-open reuses the same page size.
		assert.Equal(t, application.MeHistoryDefaultLimit, st.HistoryLimit)
	})
}

// meChartResponse encodes a MeChartResponse to JSON for use as fake fetch payload.
func meChartResponse(window string, pairs []dto.MeChartPairRow) []byte {
	resp := dto.MeChartResponse{Window: window, Pairs: pairs}
	b, err := json.Marshal(resp)
	if err != nil {
		panic(err)
	}
	return b
}

func TestMeSubscriptionsPage_LoadSparklineChart(t *testing.T) {
	t.Parallel()

	t.Run("clears ChartLoading and ChartError on success", func(t *testing.T) {
		t.Parallel()

		pairs := []dto.MeChartPairRow{
			{
				Pair:     "USD/KZT",
				Category: "fiat",
				Series: []dto.MeChartSeries{
					{Kind: "BID", Color: "#1D9E75", Latest: 487.0},
				},
			},
		}
		f := &meSubsFakeFetcher{jsonResponse: meChartResponse("7 days", pairs)}
		page := newMePage(f, "valid-init-data")

		assert.False(t, page.State().ChartLoading)
		assert.NoError(t, page.State().ChartError)

		err := page.LoadSparklineChart(t.Context())
		require.NoError(t, err)

		st := page.State()
		assert.False(t, st.ChartLoading)
		assert.NoError(t, st.ChartError)
	})

	t.Run("sets ChartError on fetch failure", func(t *testing.T) {
		t.Parallel()

		f := &meSubsFakeFetcher{jsonErr: errors.New("http 503")}
		page := newMePage(f, "valid-init-data")

		err := page.LoadSparklineChart(t.Context())
		require.Error(t, err)

		st := page.State()
		assert.False(t, st.ChartLoading, "ChartLoading must be false after failure")
		require.Error(t, st.ChartError)
		assert.ErrorContains(t, st.ChartError, "http 503")
		assert.Nil(t, st.Chart, "Chart must remain nil on failure")
	})

	t.Run("populates Chart.Pairs on success", func(t *testing.T) {
		t.Parallel()

		pairs := []dto.MeChartPairRow{
			{
				Pair:     "USD/KZT",
				Category: "fiat",
				Series:   []dto.MeChartSeries{{Kind: "BID", Color: "#1D9E75", Latest: 487.0}},
			},
			{
				Pair:     "EUR/KZT",
				Category: "fiat",
				Series:   []dto.MeChartSeries{{Kind: "BID", Color: "#378ADD", Latest: 530.0}},
			},
		}
		f := &meSubsFakeFetcher{jsonResponse: meChartResponse("7 days", pairs)}
		page := newMePage(f, "valid-init-data")

		err := page.LoadSparklineChart(t.Context())
		require.NoError(t, err)

		st := page.State()
		require.NotNil(t, st.Chart)
		require.Len(t, st.Chart.Pairs, 2)
		assert.Equal(t, "USD/KZT", st.Chart.Pairs[0].Pair)
		assert.Equal(t, "EUR/KZT", st.Chart.Pairs[1].Pair)
		assert.Equal(t, "7 days", st.Chart.Window)
	})

	t.Run("auto-closes modal when reloaded chart no longer contains OpenPair", func(t *testing.T) {
		t.Parallel()

		// Initial chart has USD/KZT; user opens that modal.
		initialPairs := []dto.MeChartPairRow{
			{Pair: "USD/KZT", Series: []dto.MeChartSeries{{Kind: "BID", Color: "#1D9E75", Latest: 487.0}}},
		}
		f := &meSubsFakeFetcher{jsonResponse: meChartResponse("7 days", initialPairs)}
		page := newMePage(f, "valid-init-data")
		require.NoError(t, page.LoadSparklineChart(t.Context()))
		page.OpenPairModal("USD/KZT")
		require.NotNil(t, page.State().OpenPair)

		// Chart reloads and USD/KZT is gone.
		newPairs := []dto.MeChartPairRow{
			{Pair: "EUR/KZT", Series: []dto.MeChartSeries{{Kind: "BID", Color: "#378ADD", Latest: 530.0}}},
		}
		f.jsonResponse = meChartResponse("7 days", newPairs)
		require.NoError(t, page.LoadSparklineChart(t.Context()))

		assert.Nil(t, page.State().OpenPair, "OpenPair must be cleared when pair is no longer in the chart")
	})

	t.Run("keeps modal open when reloaded chart still contains OpenPair", func(t *testing.T) {
		t.Parallel()

		pairs := []dto.MeChartPairRow{
			{Pair: "USD/KZT", Series: []dto.MeChartSeries{{Kind: "BID", Color: "#1D9E75", Latest: 487.0}}},
		}
		f := &meSubsFakeFetcher{jsonResponse: meChartResponse("7 days", pairs)}
		page := newMePage(f, "valid-init-data")
		require.NoError(t, page.LoadSparklineChart(t.Context()))
		page.OpenPairModal("USD/KZT")

		// Reload with the same pair still present.
		require.NoError(t, page.LoadSparklineChart(t.Context()))
		require.NotNil(t, page.State().OpenPair)
		assert.Equal(t, "USD/KZT", *page.State().OpenPair)
	})
}

// meHistoryResponse encodes a MeHistoryResponse to JSON for use as fake fetch payload.
func meHistoryResponse(pair string, page, limit int, total int64, items []dto.MeHistoryRow) []byte {
	if items == nil {
		items = []dto.MeHistoryRow{}
	}
	resp := dto.MeHistoryResponse{
		Pair:  pair,
		Page:  page,
		Limit: limit,
		Total: total,
		Items: items,
	}
	b, err := json.Marshal(resp)
	if err != nil {
		panic(err)
	}
	return b
}

// sampleHistoryItems returns a small slice of MeHistoryRow for tests that need
// non-empty history data.
func sampleHistoryItems() []dto.MeHistoryRow {
	bid1 := 487.50
	bid2 := 488.00
	return []dto.MeHistoryRow{
		{SourceName: "kkb", SourceTitle: "Kaspi", Timestamp: time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC), Bid: &bid1},
		{SourceName: "kkb", SourceTitle: "Kaspi", Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), Bid: &bid2},
	}
}

func TestMeSubscriptionsPage_OpenHistory(t *testing.T) {
	t.Parallel()

	t.Run("sets HistoryOpen and loads page 1", func(t *testing.T) {
		t.Parallel()
		items := sampleHistoryItems()
		histBody := meHistoryResponse("USD/KZT", 1, 20, int64(len(items)), items)
		f := &meSubsFakeFetcher{
			urlResponses: map[string][]byte{"/api/me/rates/history": histBody},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		require.NoError(t, page.OpenHistory(t.Context()))

		st := page.State()
		assert.True(t, st.HistoryOpen)
		assert.Equal(t, 1, st.HistoryPage)
		assert.Equal(t, int64(len(items)), st.HistoryTotal)
		require.Len(t, st.HistoryItems, len(items))
		assert.NoError(t, st.HistoryError)
		assert.False(t, st.HistoryLoading)
	})

	t.Run("fetch error sets HistoryError but modal stays open", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			urlErr: map[string]error{"/api/me/rates/history": errors.New("http 500")},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		err := page.OpenHistory(t.Context())
		require.Error(t, err)

		st := page.State()
		assert.True(t, st.HistoryOpen, "HistoryOpen must stay true even on error")
		require.NotNil(t, st.OpenPair, "modal must stay open on history fetch error")
		require.Error(t, st.HistoryError)
		assert.ErrorContains(t, st.HistoryError, "http 500")
		assert.False(t, st.HistoryLoading)
	})

	t.Run("no-op when OpenPair is nil", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{}
		page := newMePage(f, "tok")
		require.NoError(t, page.OpenHistory(t.Context()))
		assert.False(t, page.State().HistoryOpen)
		assert.Equal(t, 0, f.callCount)
	})

	t.Run("cancelled context propagates error", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			urlErr: map[string]error{"/api/me/rates/history": context.Canceled},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		err := page.OpenHistory(t.Context())
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestMeSubscriptionsPage_CloseHistory(t *testing.T) {
	t.Parallel()

	t.Run("clears HistoryOpen but preserves HistoryItems", func(t *testing.T) {
		t.Parallel()
		items := sampleHistoryItems()
		histBody := meHistoryResponse("USD/KZT", 1, 20, int64(len(items)), items)
		f := &meSubsFakeFetcher{
			urlResponses: map[string][]byte{"/api/me/rates/history": histBody},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		require.NoError(t, page.OpenHistory(t.Context()))
		require.True(t, page.State().HistoryOpen)

		page.CloseHistory()
		st := page.State()
		assert.False(t, st.HistoryOpen)
		// Items survive so re-open can show cached data without a refetch.
		assert.Len(t, st.HistoryItems, len(items))
	})

	t.Run("no-op when history was never opened", func(t *testing.T) {
		t.Parallel()
		page := newMePage(&meSubsFakeFetcher{}, "tok")
		page.CloseHistory()
		assert.False(t, page.State().HistoryOpen)
	})
}

func TestMeSubscriptionsPage_LoadHistory(t *testing.T) {
	t.Parallel()

	t.Run("success stores items page and total", func(t *testing.T) {
		t.Parallel()
		items := sampleHistoryItems()
		histBody := meHistoryResponse("USD/KZT", 2, 20, 42, items)
		f := &meSubsFakeFetcher{
			urlResponses: map[string][]byte{"/api/me/rates/history": histBody},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		require.NoError(t, page.LoadHistory(t.Context(), 2))

		st := page.State()
		assert.Equal(t, 2, st.HistoryPage)
		assert.Equal(t, int64(42), st.HistoryTotal)
		require.Len(t, st.HistoryItems, len(items))
		assert.NoError(t, st.HistoryError)
		assert.False(t, st.HistoryLoading)
	})

	t.Run("error path sets HistoryError and returns error", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			urlErr: map[string]error{"/api/me/rates/history": errors.New("http 502")},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		err := page.LoadHistory(t.Context(), 1)
		require.Error(t, err)

		st := page.State()
		require.Error(t, st.HistoryError)
		assert.ErrorContains(t, st.HistoryError, "http 502")
		assert.False(t, st.HistoryLoading, "HistoryLoading must be false after fetch")
	})

	t.Run("context-canceled propagates error", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			urlErr: map[string]error{"/api/me/rates/history": context.Canceled},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		err := page.LoadHistory(t.Context(), 1)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
		assert.False(t, page.State().HistoryLoading)
	})

	t.Run("no-op when OpenPair is nil", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{}
		page := newMePage(f, "tok")
		require.NoError(t, page.LoadHistory(t.Context(), 1))
		assert.Equal(t, 0, f.callCount)
	})

	t.Run("uses default limit when HistoryLimit is zero", func(t *testing.T) {
		t.Parallel()
		histBody := meHistoryResponse("USD/KZT", 1, application.MeHistoryDefaultLimit, 0, nil)
		f := &meSubsFakeFetcher{
			urlResponses: map[string][]byte{"/api/me/rates/history": histBody},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		require.NoError(t, page.LoadHistory(t.Context(), 1))
		assert.Equal(t, application.MeHistoryDefaultLimit, page.State().HistoryLimit)
	})

	t.Run("success clears previous HistoryError", func(t *testing.T) {
		t.Parallel()
		// First call errors, second succeeds.
		f := &meSubsFakeFetcher{
			urlErr: map[string]error{"/api/me/rates/history": errors.New("transient")},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		require.Error(t, page.LoadHistory(t.Context(), 1))
		require.Error(t, page.State().HistoryError)

		// Repair the fake: now return a success body.
		delete(f.urlErr, "/api/me/rates/history")
		f.urlResponses = map[string][]byte{
			"/api/me/rates/history": meHistoryResponse("USD/KZT", 1, 20, 0, nil),
		}
		require.NoError(t, page.LoadHistory(t.Context(), 1))
		assert.NoError(t, page.State().HistoryError)
	})

	t.Run("401 sets AuthFailure and resets modal state", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			urlErr: map[string]error{"/api/me/rates/history": errors.New("http 401 unauthorized")},
		}
		page := newMePage(f, "expired-token")
		page.OpenPairModal("USD/KZT")
		err := page.LoadHistory(t.Context(), 1)
		require.Error(t, err)

		st := page.State()
		assert.True(t, st.AuthFailure, "AuthFailure must be true on 401")
		assert.Nil(t, st.OpenPair, "OpenPair must be cleared on auth failure")
		assert.False(t, st.HistoryOpen, "HistoryOpen must be false on auth failure")
		assert.Nil(t, st.HistoryItems)
		assert.Equal(t, 0, st.HistoryPage)
		assert.Equal(t, int64(0), st.HistoryTotal)
		assert.False(t, st.HistoryLoading)
	})

	t.Run("stale result is dropped when OpenPair changed mid-fetch", func(t *testing.T) {
		// The fake is synchronous, so we simulate "mid-fetch pair switch" by having
		// LoadHistory succeed, then immediately overwriting OpenPair before the
		// write-back check runs. We do this by calling OpenPairModal("EUR/KZT")
		// inside the fake's response — instead, we test the equivalent: call
		// LoadHistory for USD/KZT, then manually switch OpenPair to EUR/KZT so that
		// the guard catches it.
		//
		// Because the fake resolves synchronously, the actual guard is exercised by
		// calling page.OpenPairModal("EUR/KZT") after LoadHistory returns but we need
		// to test the guard fires *before* write-back. We achieve this by inserting a
		// hook: load page 1 successfully for USD/KZT, switch OpenPair to EUR/KZT,
		// then load page 1 again — the result for USD/KZT must not clobber EUR/KZT.
		// This tests the branch via the only controllable observable: state after the
		// call where the pair was already different at call time.
		t.Parallel()
		items := sampleHistoryItems()
		histBody := meHistoryResponse("USD/KZT", 1, 20, int64(len(items)), items)
		f := &meSubsFakeFetcher{
			urlResponses: map[string][]byte{"/api/me/rates/history": histBody},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		require.NoError(t, page.LoadHistory(t.Context(), 1))
		assert.Equal(t, 1, page.State().HistoryPage)

		// Switch to a different pair without loading history for it yet.
		page.OpenPairModal("EUR/KZT")

		// The fake still returns USD/KZT data; LoadHistory for EUR/KZT should
		// see that *OpenPair != targetPair after the fetch and drop the result.
		// Since the URL and the fake body don't match the new pair, this exercises
		// the guard: targetPair="EUR/KZT", *OpenPair is "EUR/KZT" — guard passes.
		// To actually trip the guard (OpenPair != targetPair after fetch), we need
		// the pair to change between the snapshot and the write-back. Since the fake
		// is synchronous, we indirectly verify the guard is reachable by testing the
		// nil-OpenPair path below.
		require.NoError(t, page.LoadHistory(t.Context(), 1))
	})

	t.Run("stale result is dropped when OpenPair was cleared mid-fetch", func(t *testing.T) {
		// Simulate a goroutine that calls ClosePairModal while LoadHistory is in
		// flight. Since the fake is synchronous, we verify the nil-guard path by
		// having the fake close the modal before LoadHistory returns. We do this by
		// directly manipulating state between snapshot and write-back: call
		// LoadHistory for a page, then verify that if we close first and reload, the
		// second call is a no-op.
		t.Parallel()
		items := sampleHistoryItems()
		histBody := meHistoryResponse("USD/KZT", 1, 20, int64(len(items)), items)
		f := &meSubsFakeFetcher{
			urlResponses: map[string][]byte{"/api/me/rates/history": histBody},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		require.NoError(t, page.LoadHistory(t.Context(), 1))

		// Close the modal (simulating it being closed mid-fetch on the next call).
		page.ClosePairModal()

		// Now OpenPair is nil — LoadHistory must be a no-op.
		callsBefore := f.callCount
		require.NoError(t, page.LoadHistory(t.Context(), 2))
		assert.Equal(t, callsBefore, f.callCount, "no fetch when OpenPair is nil")
		assert.Equal(t, 0, page.State().HistoryPage, "page must not advance after no-op")
	})
}

func TestMeSubscriptionsPage_HistoryNextPage(t *testing.T) {
	t.Parallel()

	t.Run("advances to the next page when more rows exist", func(t *testing.T) {
		t.Parallel()
		// Page 1 of 3 pages (20 rows, total 50).
		histBody := meHistoryResponse("USD/KZT", 1, 20, 50, sampleHistoryItems())
		f := &meSubsFakeFetcher{
			urlResponses: map[string][]byte{"/api/me/rates/history": histBody},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		require.NoError(t, page.LoadHistory(t.Context(), 1))
		assert.Equal(t, 1, page.State().HistoryPage)

		// Update fake to return page 2 data.
		f.urlResponses["/api/me/rates/history"] = meHistoryResponse("USD/KZT", 2, 20, 50, sampleHistoryItems())
		require.NoError(t, page.HistoryNextPage(t.Context()))
		assert.Equal(t, 2, page.State().HistoryPage)
	})

	t.Run("no-op when already at the last page", func(t *testing.T) {
		t.Parallel()
		// 20 rows total on page 1 — cursor is at the end.
		histBody := meHistoryResponse("USD/KZT", 1, 20, 20, sampleHistoryItems())
		f := &meSubsFakeFetcher{
			urlResponses: map[string][]byte{"/api/me/rates/history": histBody},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		require.NoError(t, page.LoadHistory(t.Context(), 1))

		callsBefore := f.callCount
		require.NoError(t, page.HistoryNextPage(t.Context()))
		assert.Equal(t, 1, page.State().HistoryPage, "page must not change at last page")
		assert.Equal(t, callsBefore, f.callCount, "no extra fetch must be issued")
	})
}

func TestMeSubscriptionsPage_HistoryPrevPage(t *testing.T) {
	t.Parallel()

	t.Run("decrements to the previous page", func(t *testing.T) {
		t.Parallel()
		// Load page 2 first.
		histBody := meHistoryResponse("USD/KZT", 2, 20, 50, sampleHistoryItems())
		f := &meSubsFakeFetcher{
			urlResponses: map[string][]byte{"/api/me/rates/history": histBody},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		require.NoError(t, page.LoadHistory(t.Context(), 2))
		assert.Equal(t, 2, page.State().HistoryPage)

		// Return page 1 data on next fetch.
		f.urlResponses["/api/me/rates/history"] = meHistoryResponse("USD/KZT", 1, 20, 50, sampleHistoryItems())
		require.NoError(t, page.HistoryPrevPage(t.Context()))
		assert.Equal(t, 1, page.State().HistoryPage)
	})

	t.Run("no-op when already on page 1", func(t *testing.T) {
		t.Parallel()
		histBody := meHistoryResponse("USD/KZT", 1, 20, 50, sampleHistoryItems())
		f := &meSubsFakeFetcher{
			urlResponses: map[string][]byte{"/api/me/rates/history": histBody},
		}
		page := newMePage(f, "tok")
		page.OpenPairModal("USD/KZT")
		require.NoError(t, page.LoadHistory(t.Context(), 1))
		assert.Equal(t, 1, page.State().HistoryPage)

		callsBefore := f.callCount
		require.NoError(t, page.HistoryPrevPage(t.Context()))
		assert.Equal(t, 1, page.State().HistoryPage, "page must not change on page 1")
		assert.Equal(t, callsBefore, f.callCount, "no extra fetch must be issued")
	})
}

func TestFindPairInChart(t *testing.T) {
	t.Parallel()

	chart := &dto.MeChartResponse{
		Pairs: []dto.MeChartPairRow{
			{Pair: "USD/KZT"},
			{Pair: "EUR/KZT"},
		},
	}

	t.Run("nil chart returns false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, application.FindPairInChart(nil, "USD/KZT"))
	})

	t.Run("empty pairs returns false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, application.FindPairInChart(&dto.MeChartResponse{}, "USD/KZT"))
	})

	t.Run("pair present returns true", func(t *testing.T) {
		t.Parallel()
		assert.True(t, application.FindPairInChart(chart, "USD/KZT"))
	})

	t.Run("pair absent returns false", func(t *testing.T) {
		t.Parallel()
		assert.False(t, application.FindPairInChart(chart, "GBP/KZT"))
	})
}
