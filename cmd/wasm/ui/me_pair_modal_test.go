package ui_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/seilbekskindirov/monitor/cmd/wasm/application"
	"github.com/seilbekskindirov/monitor/cmd/wasm/ui"
	"github.com/seilbekskindirov/monitor/internal/domain/ratepair"
	"github.com/seilbekskindirov/monitor/internal/dto"
)

// pairModalState is a helper that returns a MeSubscriptionsState with OpenPair
// set to pair and Chart populated with the given pairs slice.
func pairModalState(openPair string, chartPairs []dto.MeChartPairRow, items []dto.MeSubscriptionRow) application.MeSubscriptionsState {
	p := openPair
	chart := &dto.MeChartResponse{Window: "7d", Pairs: chartPairs}
	return application.MeSubscriptionsState{
		OpenPair: &p,
		Chart:    chart,
		Items:    items,
	}
}

func TestRenderPairModal(t *testing.T) {
	t.Parallel()

	t.Run("returns empty string when OpenPair is nil", func(t *testing.T) {
		t.Parallel()
		state := application.MeSubscriptionsState{}
		assert.Equal(t, "", ui.RenderPairModal(state))
	})

	t.Run("returns empty string when OpenPair does not match any chart pair", func(t *testing.T) {
		t.Parallel()
		bid := mkSeries("BID", ratepair.ColorBid, 1.0, false, []dto.MeChartPoint{
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		})
		row := mkRow("USD/KZT", "fiat", nil, []dto.MeChartSeries{bid})
		state := pairModalState("EUR/KZT", []dto.MeChartPairRow{row}, nil)
		assert.Equal(t, "", ui.RenderPairModal(state))
	})

	t.Run("returns empty string when chart is nil", func(t *testing.T) {
		t.Parallel()
		p := "USD/KZT"
		state := application.MeSubscriptionsState{OpenPair: &p, Chart: nil}
		assert.Equal(t, "", ui.RenderPairModal(state))
	})

	t.Run("renders role=dialog aria-modal and aria-labelledby", func(t *testing.T) {
		t.Parallel()
		bid := mkSeries("BID", ratepair.ColorBid, 1.0, false, []dto.MeChartPoint{
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		})
		row := mkRow("USD/KZT", "fiat", nil, []dto.MeChartSeries{bid})
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, nil)
		html := ui.RenderPairModal(state)

		assert.Contains(t, html, `role="dialog"`)
		assert.Contains(t, html, `aria-modal="true"`)
		assert.Contains(t, html, `aria-labelledby="me-pair-modal-title"`)
	})

	t.Run("renders close button and backdrop", func(t *testing.T) {
		t.Parallel()
		bid := mkSeries("BID", ratepair.ColorBid, 1.0, false, []dto.MeChartPoint{
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		})
		row := mkRow("USD/KZT", "fiat", nil, []dto.MeChartSeries{bid})
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, nil)
		html := ui.RenderPairModal(state)

		assert.Contains(t, html, `id="me-pair-modal-close"`)
		assert.Contains(t, html, `id="me-pair-modal-backdrop"`)
	})

	t.Run("single-series pair renders one series block and no spread line", func(t *testing.T) {
		t.Parallel()
		pts := []dto.MeChartPoint{
			mkPoint("2026-05-23T00:00:00Z", 480.0),
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		}
		bid := mkSeries("BID", ratepair.ColorBid, 1.5, false, pts)
		row := mkRow("USD/KZT", "fiat", nil, []dto.MeChartSeries{bid})
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, nil)
		html := ui.RenderPairModal(state)

		assert.Equal(t, 1, strings.Count(html, `class="me-pair-modal-series"`))
		assert.NotContains(t, html, "me-pair-modal-spread")
		// Modal is text-only — no SVG element.
		assert.NotContains(t, html, "<svg")
		// But the value line is present.
		assert.Contains(t, html, "sparkline-value-line")
	})

	t.Run("two-series pair renders two series blocks plus Spread line", func(t *testing.T) {
		t.Parallel()
		bidPts := []dto.MeChartPoint{
			mkPoint("2026-05-23T00:00:00Z", 480.0),
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		}
		askPts := []dto.MeChartPoint{
			mkPoint("2026-05-23T00:00:00Z", 481.0),
			mkPoint("2026-05-27T00:00:00Z", 488.0),
		}
		bid := mkSeries("BID", ratepair.ColorBid, 1.5, false, bidPts)
		ask := mkSeries("ASK", ratepair.ColorAsk, 1.4, false, askPts)
		row := mkRow("USD/KZT", "fiat", float64Ptr(0.21), []dto.MeChartSeries{bid, ask})
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, nil)
		html := ui.RenderPairModal(state)

		assert.Equal(t, 2, strings.Count(html, `class="me-pair-modal-series"`))
		assert.Contains(t, html, "me-pair-modal-spread")
		assert.Contains(t, html, "Spread 0.21%")
		// Modal is text-only — no SVG.
		assert.NotContains(t, html, "<svg")
	})

	t.Run("modal contains no svg element", func(t *testing.T) {
		t.Parallel()
		pts := []dto.MeChartPoint{
			mkPoint("2026-05-23T00:00:00Z", 480.0),
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		}
		bid := mkSeries("BID", ratepair.ColorBid, 1.5, false, pts)
		ask := mkSeries("ASK", ratepair.ColorAsk, 1.4, false, []dto.MeChartPoint{
			mkPoint("2026-05-23T00:00:00Z", 481.0),
			mkPoint("2026-05-27T00:00:00Z", 488.0),
		})
		row := mkRow("USD/KZT", "fiat", float64Ptr(0.21), []dto.MeChartSeries{bid, ask})
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, nil)
		html := ui.RenderPairModal(state)

		assert.NotContains(t, html, "<svg", "modal must be text-only — no SVG in detail view")
		assert.NotContains(t, html, "<polyline", "no polyline in text-only modal")
	})

	t.Run("History button is present when HistoryOpen is false", func(t *testing.T) {
		t.Parallel()
		bid := mkSeries("BID", ratepair.ColorBid, 1.0, false, []dto.MeChartPoint{
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		})
		row := mkRow("USD/KZT", "fiat", nil, []dto.MeChartSeries{bid})
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, nil)
		html := ui.RenderPairModal(state)

		assert.Contains(t, html, `id="me-pair-modal-history"`)
		assert.Contains(t, html, "me-pair-modal-actions")
	})

	t.Run("history view replaces detail body when HistoryOpen is true", func(t *testing.T) {
		t.Parallel()
		bid := mkSeries("BID", ratepair.ColorBid, 1.0, false, []dto.MeChartPoint{
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		})
		row := mkRow("USD/KZT", "fiat", nil, []dto.MeChartSeries{bid})
		p := "USD/KZT"
		chart := &dto.MeChartResponse{Window: "7d", Pairs: []dto.MeChartPairRow{row}}
		state := application.MeSubscriptionsState{
			OpenPair:    &p,
			Chart:       chart,
			HistoryOpen: true,
			HistoryPage: 1,
		}
		html := ui.RenderPairModal(state)

		// History body is present.
		assert.Contains(t, html, `id="me-pair-history-back"`)
		// Detail-only elements are absent.
		assert.NotContains(t, html, `id="me-pair-modal-history"`)
		assert.NotContains(t, html, "me-pair-modal-series")
		// Modal chrome stays.
		assert.Contains(t, html, `id="me-pair-modal-close"`)
		assert.Contains(t, html, `role="dialog"`)
		assert.Contains(t, html, `aria-modal="true"`)
	})

	t.Run("last grab line uses max timestamp across series", func(t *testing.T) {
		t.Parallel()
		// BID has the later timestamp.
		bidPts := []dto.MeChartPoint{
			mkPoint("2026-05-20T00:00:00Z", 485.0),
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		}
		askPts := []dto.MeChartPoint{
			mkPoint("2026-05-19T00:00:00Z", 486.0),
			mkPoint("2026-05-25T00:00:00Z", 488.0),
		}
		bid := mkSeries("BID", ratepair.ColorBid, 0.4, false, bidPts)
		ask := mkSeries("ASK", ratepair.ColorAsk, 0.5, false, askPts)
		row := mkRow("USD/KZT", "fiat", float64Ptr(0.2), []dto.MeChartSeries{bid, ask})
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, nil)
		html := ui.RenderPairModal(state)

		assert.Contains(t, html, "me-pair-modal-time")
		assert.Contains(t, html, "Last grab:")
		// 2026-05-27 is the latest point; the formatted output must contain that date.
		assert.Contains(t, html, "5/27/2026")
	})

	t.Run("last grab line omitted when all series are sparse with no points", func(t *testing.T) {
		t.Parallel()
		sr := dto.MeChartSeries{
			Kind:   "BID",
			Color:  ratepair.ColorBid,
			Latest: 487.0,
			Sparse: true,
			Points: nil,
		}
		row := mkRow("USD/KZT", "fiat", nil, []dto.MeChartSeries{sr})
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, nil)
		html := ui.RenderPairModal(state)

		assert.NotContains(t, html, "Last grab:")
		assert.NotContains(t, html, "me-pair-modal-time")
	})

	t.Run("condition badges from matching MeSubscriptionRow render in modal", func(t *testing.T) {
		t.Parallel()
		bid := mkSeries("BID", ratepair.ColorBid, 1.0, false, []dto.MeChartPoint{
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		})
		row := mkRow("USD/KZT", "fiat", nil, []dto.MeChartSeries{bid})
		items := []dto.MeSubscriptionRow{
			{
				SourceName:    "src",
				BaseCurrency:  "USD",
				QuoteCurrency: "KZT",
				Conditions:    []string{">490", "<500"},
			},
		}
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, items)
		html := ui.RenderPairModal(state)

		assert.Contains(t, html, `class="badges"`)
		assert.Contains(t, html, `class="badge"`)
		assert.Contains(t, html, "&gt;490")
		assert.Contains(t, html, "&lt;500")
	})

	t.Run("no badges block when no matching MeSubscriptionRow", func(t *testing.T) {
		t.Parallel()
		bid := mkSeries("BID", ratepair.ColorBid, 1.0, false, []dto.MeChartPoint{
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		})
		row := mkRow("USD/KZT", "fiat", nil, []dto.MeChartSeries{bid})
		items := []dto.MeSubscriptionRow{
			{
				SourceName:    "src",
				BaseCurrency:  "EUR",
				QuoteCurrency: "KZT",
				Conditions:    []string{">400"},
			},
		}
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, items)
		html := ui.RenderPairModal(state)
		assert.NotContains(t, html, `class="badges"`)
	})

	t.Run("XSS in pair label escaped in data-pair and title and aria-labelledby target", func(t *testing.T) {
		t.Parallel()
		hostile := `<script>alert(1)</script>`
		bid := mkSeries("BID", ratepair.ColorBid, 0.0, true, nil)
		row := mkRow(hostile, "fiat", nil, []dto.MeChartSeries{bid})
		state := pairModalState(hostile, []dto.MeChartPairRow{row}, nil)
		html := ui.RenderPairModal(state)

		assert.NotContains(t, html, "<script>")
		assert.Contains(t, html, "&lt;script&gt;")
		assert.Contains(t, html, `data-pair="&lt;script&gt;`)
		assert.Contains(t, html, `id="me-pair-modal-title"`)
	})

	t.Run("single-series row with stray SpreadPct does not render spread line", func(t *testing.T) {
		t.Parallel()
		spreadVal := 0.21
		bid := mkSeries("BID", ratepair.ColorBid, 1.0, false, []dto.MeChartPoint{
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		})
		// Single series but SpreadPct is non-nil — spread line must be suppressed.
		row := mkRow("USD/KZT", "fiat", &spreadVal, []dto.MeChartSeries{bid})
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, nil)
		html := ui.RenderPairModal(state)

		assert.NotContains(t, html, "me-pair-modal-spread")
		assert.NotContains(t, html, "Spread 0.21%")
	})

	t.Run("XSS in condition badge text escaped", func(t *testing.T) {
		t.Parallel()
		bid := mkSeries("BID", ratepair.ColorBid, 1.0, false, []dto.MeChartPoint{
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		})
		row := mkRow("USD/KZT", "fiat", nil, []dto.MeChartSeries{bid})
		items := []dto.MeSubscriptionRow{
			{
				BaseCurrency:  "USD",
				QuoteCurrency: "KZT",
				Conditions:    []string{`<img src=x onerror=alert(1)>`},
			},
		}
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, items)
		html := ui.RenderPairModal(state)

		assert.NotContains(t, html, "<img src=x")
		assert.Contains(t, html, "&lt;img src=x")
	})

	t.Run("modal output contains no script tag", func(t *testing.T) {
		t.Parallel()
		pts := []dto.MeChartPoint{
			mkPoint("2026-05-23T00:00:00Z", 480.0),
			mkPoint("2026-05-27T00:00:00Z", 487.0),
		}
		bid := mkSeries("BID", ratepair.ColorBid, 1.5, false, pts)
		row := mkRow("USD/KZT", "fiat", nil, []dto.MeChartSeries{bid})
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, nil)
		html := ui.RenderPairModal(state)
		assert.NotContains(t, strings.ToLower(html), "<script")
	})

	t.Run("flat union (max==min) renders without panic", func(t *testing.T) {
		t.Parallel()
		pts := []dto.MeChartPoint{
			mkPoint("2026-05-23T00:00:00Z", 487.0),
			mkPoint("2026-05-24T00:00:00Z", 487.0),
		}
		bid := mkSeries("BID", ratepair.ColorBid, 0.0, false, pts)
		ask := mkSeries("ASK", ratepair.ColorAsk, 0.0, false, []dto.MeChartPoint{
			mkPoint("2026-05-23T00:00:00Z", 487.0),
			mkPoint("2026-05-24T00:00:00Z", 487.0),
		})
		row := mkRow("USD/KZT", "fiat", float64Ptr(0.0), []dto.MeChartSeries{bid, ask})
		state := pairModalState("USD/KZT", []dto.MeChartPairRow{row}, nil)
		html := ui.RenderPairModal(state)
		require.NotEmpty(t, html)
		// Text-only detail: no SVG, but two series blocks.
		assert.NotContains(t, html, "<svg")
		assert.Equal(t, 2, strings.Count(html, `class="me-pair-modal-series"`))
	})
}
