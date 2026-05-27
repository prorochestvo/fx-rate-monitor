package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seilbekskindirov/monitor/cmd/wasm/apiclient"
	"github.com/seilbekskindirov/monitor/cmd/wasm/application"
	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/dto"
)

// meSubsFakeFetcher is a Fetcher that records every FetchJSON call and lets
// tests configure the response per call or globally.
type meSubsFakeFetcher struct {
	jsonResponse []byte
	jsonErr      error
	callCount    int
	lastHeaders  map[string]string
}

var _ apiclient.Fetcher = (*meSubsFakeFetcher)(nil)

func (f *meSubsFakeFetcher) FetchJSON(_ context.Context, _, _ string, _ any, headers map[string]string) ([]byte, error) {
	f.callCount++
	f.lastHeaders = headers
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

// chartPointsJSON returns a JSON-encoded []dto.ChartPointResponse for use by
// a fetcher that must serve both subscriptions and chart responses. The fetcher
// returns the same bytes for every call, so tests that need to intercept chart
// fetches use blockingFetcher instead.
func chartPointsJSON(pairs ...any) []byte {
	pts := make([]dto.ChartPointResponse, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		pts = append(pts, dto.ChartPointResponse{
			Label: pairs[i].(string),
			Price: pairs[i+1].(float64),
		})
	}
	b, err := json.Marshal(pts)
	if err != nil {
		panic(err)
	}
	return b
}

// splitFetcher lets tests return different bytes for subscriptions vs chart URLs.
type splitFetcher struct {
	subJSON   []byte
	chartJSON []byte
	chartErr  error
	callCount int
}

var _ apiclient.Fetcher = (*splitFetcher)(nil)

func (f *splitFetcher) FetchJSON(_ context.Context, _, url string, _ any, _ map[string]string) ([]byte, error) {
	f.callCount++
	if len(url) > 12 && url[len(url)-12:] == "/rates/chart" || containsChartPath(url) {
		if f.chartErr != nil {
			return nil, f.chartErr
		}
		return f.chartJSON, nil
	}
	return f.subJSON, nil
}

func (f *splitFetcher) FetchNoContent(_ context.Context, _, _ string, _ any, _ map[string]string) error {
	return nil
}

func containsChartPath(url string) bool {
	for i := range url {
		if i+6 <= len(url) && url[i:i+6] == "chart?" {
			return true
		}
		if i+5 <= len(url) && url[i:i+5] == "chart" && (len(url) == i+5) {
			return true
		}
	}
	return false
}

// blockingFetcher lets the test drive when a FetchJSON returns. The test sends
// a response on the release channel to unblock the call.
type blockingFetcher struct {
	subJSON []byte
	release chan struct {
		data []byte
		err  error
	}
	callsIn chan string // receives the URL when a chart fetch arrives
}

var _ apiclient.Fetcher = (*blockingFetcher)(nil)

func (f *blockingFetcher) FetchJSON(_ context.Context, _, url string, _ any, _ map[string]string) ([]byte, error) {
	if containsChartPath(url) {
		if f.callsIn != nil {
			f.callsIn <- url
		}
		resp := <-f.release
		return resp.data, resp.err
	}
	return f.subJSON, nil
}

func (f *blockingFetcher) FetchNoContent(_ context.Context, _, _ string, _ any, _ map[string]string) error {
	return nil
}

func TestMeSubscriptionsPage_LoadInitial(t *testing.T) {
	t.Parallel()

	t.Run("happy path stores items and total", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			jsonResponse: meSubsResponse(sampleItems(), 1, 1, 10),
		}
		page := newMePage(f, "valid-init-data")
		err := page.LoadInitial(t.Context())
		require.NoError(t, err)
		st := page.State()
		assert.Len(t, st.Items, 1)
		assert.Equal(t, int64(1), st.Total)
		assert.Equal(t, 1, st.Page)
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
		assert.Equal(t, int64(0), st.Total)
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

	t.Run("resets page to 1 regardless of prior state", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			jsonResponse: meSubsResponse(sampleItems(), 5, 1, 10),
		}
		page := newMePage(f, "tok")
		// Simulate user having navigated to page 3 before calling LoadInitial.
		err := page.NextPage(t.Context())
		require.NoError(t, err)
		err = page.NextPage(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 3, page.State().Page)

		err = page.LoadInitial(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, page.State().Page)
	})
}

func TestMeSubscriptionsPage_NextPage(t *testing.T) {
	t.Parallel()

	t.Run("increments page and fetches", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			jsonResponse: meSubsResponse(sampleItems(), 30, 2, 10),
		}
		page := newMePage(f, "tok")
		err := page.NextPage(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 2, page.State().Page)
	})

	t.Run("fetch error propagates and stores LastError", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{jsonErr: errors.New("http 500")}
		page := newMePage(f, "tok")
		err := page.NextPage(t.Context())
		require.Error(t, err)
		assert.ErrorContains(t, page.State().LastError, "http 500")
		// Page is still incremented even on error — matches JS behaviour where
		// currentPage++ happens before the fetch attempt.
		assert.Equal(t, 2, page.State().Page)
	})
}

func TestMeSubscriptionsPage_PrevPage(t *testing.T) {
	t.Parallel()

	t.Run("decrements page and fetches when page > 1", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			jsonResponse: meSubsResponse(sampleItems(), 30, 1, 10),
		}
		page := newMePage(f, "tok")
		// Advance to page 2 first.
		f.jsonResponse = meSubsResponse(sampleItems(), 30, 2, 10)
		err := page.NextPage(t.Context())
		require.NoError(t, err)
		require.Equal(t, 2, page.State().Page)

		f.jsonResponse = meSubsResponse(sampleItems(), 30, 1, 10)
		err = page.PrevPage(t.Context())
		require.NoError(t, err)
		assert.Equal(t, 1, page.State().Page)
	})

	t.Run("no-op when already on page 1", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			jsonResponse: meSubsResponse(sampleItems(), 10, 1, 10),
		}
		page := newMePage(f, "tok")
		err := page.LoadInitial(t.Context())
		require.NoError(t, err)
		callsBefore := f.callCount

		err = page.PrevPage(t.Context())
		require.NoError(t, err)
		// No additional fetch should have been issued.
		assert.Equal(t, callsBefore, f.callCount, "PrevPage at page 1 must not issue a fetch")
		assert.Equal(t, 1, page.State().Page)
	})
}

func TestMeSubscriptionsPage_OnSearch(t *testing.T) {
	t.Parallel()

	t.Run("debounce fires exactly once for rapid keystrokes", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			jsonResponse: meSubsResponse(sampleItems(), 1, 1, 10),
		}
		page := newMePage(f, "tok")

		// Call OnSearch twice in rapid succession within 100 ms.
		// Only the second search ("us") should result in a network call.
		// The first call's timer is cancelled by the second call (100 ms < 250 ms
		// debounce), so the first channel never sends; we capture but do not read it.
		firstDone := page.OnSearch("usd")
		time.Sleep(100 * time.Millisecond)
		done := page.OnSearch("us")
		// firstDone is intentionally not read: the debounce timer for the first
		// call was cancelled before it fired, so the channel will never receive.
		_ = firstDone

		var searchErr error
		select {
		case searchErr = <-done:
		case <-time.After(600 * time.Millisecond):
			t.Fatal("OnSearch did not fire within 600ms")
		}

		require.NoError(t, searchErr)
		// The fakeFetcher records every FetchJSON call. Only 1 is expected.
		assert.Equal(t, 1, f.callCount, "debounce must fire exactly one fetch for rapid keystrokes")
		assert.Equal(t, "us", page.State().Query)
	})

	t.Run("single search settles after 250ms", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{
			jsonResponse: meSubsResponse(sampleItems(), 1, 1, 10),
		}
		page := newMePage(f, "tok")

		done := page.OnSearch("eur")
		var searchErr error
		select {
		case searchErr = <-done:
		case <-time.After(600 * time.Millisecond):
			t.Fatal("OnSearch did not fire within 600ms")
		}

		require.NoError(t, searchErr)
		st := page.State()
		assert.Equal(t, "eur", st.Query)
		assert.Equal(t, 1, st.Page, "OnSearch must reset page to 1")
		assert.Len(t, st.Items, 1)
	})

	t.Run("OnSearch 401 sets AuthFailure via debounce and returns error", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{jsonErr: errors.New("http 401")}
		page := newMePage(f, "bad-tok")

		done := page.OnSearch("usd")
		var searchErr error
		select {
		case searchErr = <-done:
		case <-time.After(600 * time.Millisecond):
			t.Fatal("OnSearch did not fire within 600ms")
		}

		require.Error(t, searchErr)
		assert.ErrorContains(t, searchErr, "http 401")
		st := page.State()
		assert.True(t, st.AuthFailure)
	})
}

func TestMeSubscriptionsPage_HeaderPropagation(t *testing.T) {
	t.Parallel()

	t.Run("X-Telegram-Init-Data header matches constructor parameter", func(t *testing.T) {
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
}

func TestMeSubscriptionsPage_SetPeriod(t *testing.T) {
	t.Parallel()

	t.Run("valid period updates state and resets chart", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{jsonResponse: meSubsResponse(sampleItems(), 1, 1, 10)}
		page := newMePage(f, "tok")
		require.NoError(t, page.LoadInitial(t.Context()))

		err := page.SetPeriod(t.Context(), application.MeSubscriptionsPeriodMonth)
		require.NoError(t, err)
		st := page.State()
		assert.Equal(t, application.MeSubscriptionsPeriodMonth, st.Period)
		assert.True(t, st.Chart.Loading, "chart should be in loading state after period change")
		assert.Empty(t, st.Chart.Series)
		assert.Empty(t, st.Chart.Errors)
	})

	t.Run("unchanged period is a no-op", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{jsonResponse: meSubsResponse(sampleItems(), 1, 1, 10)}
		page := newMePage(f, "tok")
		require.NoError(t, page.LoadInitial(t.Context()))

		genBefore := page.SnapshotGeneration()
		err := page.SetPeriod(t.Context(), application.MeSubscriptionsPeriodWeek)
		require.NoError(t, err)
		assert.Equal(t, genBefore, page.SnapshotGeneration(), "generation must not change when period is unchanged")
	})

	t.Run("invalid period returns PublicError and state is unchanged", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{jsonResponse: meSubsResponse(sampleItems(), 1, 1, 10)}
		page := newMePage(f, "tok")

		err := page.SetPeriod(t.Context(), "quarterly")
		require.Error(t, err)
		var pubErr *internal.PublicError
		assert.True(t, errors.As(err, &pubErr), "error must be a PublicError")
		assert.Equal(t, "Invalid period.", pubErr.Details())
		assert.Equal(t, application.MeSubscriptionsPeriodWeek, page.State().Period, "period must be unchanged")
	})

	t.Run("period change increments generation", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{jsonResponse: meSubsResponse(sampleItems(), 1, 1, 10)}
		page := newMePage(f, "tok")
		genBefore := page.SnapshotGeneration()

		require.NoError(t, page.SetPeriod(t.Context(), application.MeSubscriptionsPeriodYear))
		assert.Greater(t, page.SnapshotGeneration(), genBefore)
	})
}

func TestMeSubscriptionsPage_LoadChart(t *testing.T) {
	t.Parallel()

	t.Run("happy path appends series and color is from palette", func(t *testing.T) {
		t.Parallel()
		subJSON := meSubsResponse(sampleItems(), 1, 1, 10)
		chartJSON := chartPointsJSON("2026-01-01", 1.0, "2026-01-02", 1.1)
		sf := &splitFetcher{subJSON: subJSON, chartJSON: chartJSON}
		page := application.NewMeSubscriptionsPage(apiclient.New(sf), "tok", 10)
		require.NoError(t, page.LoadInitial(t.Context()))

		gen := page.BeginChartLoad(1)
		err := page.LoadChart(t.Context(), "usd-eur", gen)
		require.NoError(t, err)

		st := page.State()
		require.Len(t, st.Chart.Series, 1)
		assert.NotEmpty(t, st.Chart.Series[0].Color, "series must have a color assigned")
		assert.True(t, st.Chart.Loaded)
	})

	t.Run("fetch error stores per-source error and does not poison other series", func(t *testing.T) {
		t.Parallel()
		items := []dto.MeSubscriptionRow{
			{SourceName: "usd-eur", SourceTitle: "USD/EUR"},
			{SourceName: "gbp-usd", SourceTitle: "GBP/USD"},
		}
		subJSON := meSubsResponse(items, 2, 1, 10)
		sf := &splitFetcher{
			subJSON:  subJSON,
			chartErr: errors.New("timeout"),
		}
		page := application.NewMeSubscriptionsPage(apiclient.New(sf), "tok", 10)
		require.NoError(t, page.LoadInitial(t.Context()))

		gen := page.BeginChartLoad(1)
		err := page.LoadChart(t.Context(), "usd-eur", gen)
		require.Error(t, err)

		st := page.State()
		assert.NotNil(t, st.Chart.Errors["usd-eur"], "error must be stored per source")
		assert.Empty(t, st.Chart.Series, "no series should be added on error")
	})

	t.Run("stale generation at entry returns stale sentinel without mutating state", func(t *testing.T) {
		t.Parallel()
		subJSON := meSubsResponse(sampleItems(), 1, 1, 10)
		chartJSON := chartPointsJSON("2026-01-01", 1.0)
		sf := &splitFetcher{subJSON: subJSON, chartJSON: chartJSON}
		page := application.NewMeSubscriptionsPage(apiclient.New(sf), "tok", 10)
		require.NoError(t, page.LoadInitial(t.Context()))

		gen := page.BeginChartLoad(1)
		// Bump generation so the captured gen is now stale.
		_ = page.BeginChartLoad(1)

		err := page.LoadChart(t.Context(), "usd-eur", gen)
		assert.ErrorIs(t, err, application.ErrStaleGeneration)
		assert.Empty(t, page.State().Chart.Series)
	})

	t.Run("stale generation after fetch returns stale sentinel without mutating state", func(t *testing.T) {
		t.Parallel()
		subJSON := meSubsResponse(sampleItems(), 1, 1, 10)
		// The release channel lets us control when the fetch unblocks.
		release := make(chan struct {
			data []byte
			err  error
		}, 1)
		bf := &blockingFetcher{
			subJSON: subJSON,
			release: release,
		}
		page := application.NewMeSubscriptionsPage(apiclient.New(bf), "tok", 10)
		require.NoError(t, page.LoadInitial(t.Context()))

		gen := page.BeginChartLoad(1)

		// Launch LoadChart in a goroutine; it will block waiting for release.
		done := make(chan error, 1)
		go func() {
			done <- page.LoadChart(t.Context(), "usd-eur", gen)
		}()

		// Before unblocking: bump generation so the post-fetch guard fires.
		_ = page.BeginChartLoad(1)

		// Unblock the fetch.
		release <- struct {
			data []byte
			err  error
		}{
			data: chartPointsJSON("2026-01-01", 1.0),
		}

		err := <-done
		assert.ErrorIs(t, err, application.ErrStaleGeneration, "post-fetch stale check must return ErrStaleGeneration")
		// State must not have been mutated by the stale result.
		assert.Empty(t, page.State().Chart.Series)
	})

	t.Run("two LoadChart calls produce series in stable name order", func(t *testing.T) {
		t.Parallel()
		items := []dto.MeSubscriptionRow{
			{SourceName: "zzz-source", SourceTitle: "ZZZ"},
			{SourceName: "aaa-source", SourceTitle: "AAA"},
		}
		subJSON := meSubsResponse(items, 2, 1, 10)
		chartJSON := chartPointsJSON("2026-01-01", 1.0, "2026-01-02", 1.1)
		sf := &splitFetcher{subJSON: subJSON, chartJSON: chartJSON}
		page := application.NewMeSubscriptionsPage(apiclient.New(sf), "tok", 10)
		require.NoError(t, page.LoadInitial(t.Context()))

		gen := page.BeginChartLoad(2)
		require.NoError(t, page.LoadChart(t.Context(), "zzz-source", gen))
		require.NoError(t, page.LoadChart(t.Context(), "aaa-source", gen))

		st := page.State()
		require.Len(t, st.Chart.Series, 2)
		assert.Equal(t, "AAA", st.Chart.Series[0].Name, "AAA must come first in alphabetical order")
		assert.Equal(t, "ZZZ", st.Chart.Series[1].Name)
	})

	t.Run("source name maps deterministically to palette color", func(t *testing.T) {
		t.Parallel()
		subJSON := meSubsResponse(sampleItems(), 1, 1, 10)
		chartJSON := chartPointsJSON("2026-01-01", 1.0)
		sf := &splitFetcher{subJSON: subJSON, chartJSON: chartJSON}
		page := application.NewMeSubscriptionsPage(apiclient.New(sf), "tok", 10)
		require.NoError(t, page.LoadInitial(t.Context()))

		gen := page.BeginChartLoad(1)
		require.NoError(t, page.LoadChart(t.Context(), "usd-eur", gen))
		color1 := page.State().Chart.Series[0].Color

		// Reset and load again.
		gen2 := page.BeginChartLoad(1)
		require.NoError(t, page.LoadChart(t.Context(), "usd-eur", gen2))
		color2 := page.State().Chart.Series[0].Color

		assert.Equal(t, color1, color2, "same source in same position must get the same color")
	})

	t.Run("series with SourceTitle empty falls back to SourceName", func(t *testing.T) {
		t.Parallel()
		items := []dto.MeSubscriptionRow{
			{SourceName: "no-title", SourceTitle: ""},
		}
		subJSON := meSubsResponse(items, 1, 1, 10)
		chartJSON := chartPointsJSON("2026-01-01", 1.0)
		sf := &splitFetcher{subJSON: subJSON, chartJSON: chartJSON}
		page := application.NewMeSubscriptionsPage(apiclient.New(sf), "tok", 10)
		require.NoError(t, page.LoadInitial(t.Context()))

		gen := page.BeginChartLoad(1)
		require.NoError(t, page.LoadChart(t.Context(), "no-title", gen))

		st := page.State()
		require.Len(t, st.Chart.Series, 1)
		assert.Equal(t, "no-title", st.Chart.Series[0].Name, "SourceName must be used when SourceTitle is empty")
	})
}

func TestMeSubscriptionsPage_ToggleListVisible(t *testing.T) {
	t.Parallel()

	t.Run("default is false", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{}
		page := newMePage(f, "tok")
		assert.False(t, page.State().ListVisible)
	})

	t.Run("first call flips to true", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{}
		page := newMePage(f, "tok")
		v := page.ToggleListVisible()
		assert.True(t, v)
		assert.True(t, page.State().ListVisible)
	})

	t.Run("second call flips back to false", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{}
		page := newMePage(f, "tok")
		page.ToggleListVisible()
		v := page.ToggleListVisible()
		assert.False(t, v)
		assert.False(t, page.State().ListVisible)
	})
}

func TestMeSubscriptionsPage_BeginChartLoad(t *testing.T) {
	t.Parallel()

	t.Run("increments generation and sets Loading", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{}
		page := newMePage(f, "tok")
		genBefore := page.SnapshotGeneration()
		gen := page.BeginChartLoad(3)
		assert.Greater(t, gen, genBefore)
		assert.Equal(t, gen, page.SnapshotGeneration())
		assert.True(t, page.State().Chart.Loading)
		assert.NotNil(t, page.State().Chart.Errors, "Errors map must be non-nil")
	})

	t.Run("expected zero clears Loading", func(t *testing.T) {
		t.Parallel()
		f := &meSubsFakeFetcher{}
		page := newMePage(f, "tok")
		page.BeginChartLoad(0)
		assert.False(t, page.State().Chart.Loading, "Loading must be false when expected=0")
	})
}
