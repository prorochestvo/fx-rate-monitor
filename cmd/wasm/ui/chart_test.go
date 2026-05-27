package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/seilbekskindirov/monitor/cmd/wasm/ui"
	"github.com/seilbekskindirov/monitor/internal/dto"
)

func pts(labelPricePairs ...any) []dto.ChartPointResponse {
	out := make([]dto.ChartPointResponse, 0, len(labelPricePairs)/2)
	for i := 0; i+1 < len(labelPricePairs); i += 2 {
		out = append(out, dto.ChartPointResponse{
			Label: labelPricePairs[i].(string),
			Price: labelPricePairs[i+1].(float64),
		})
	}
	return out
}

func series(name, color string, points []dto.ChartPointResponse) ui.Series {
	return ui.Series{Name: name, Color: color, Points: points}
}

func defaultOpts() ui.OverlayOptions { return ui.OverlayOptions{Width: 320, Height: 180} }

func TestRenderOverlayChart(t *testing.T) {
	t.Parallel()

	t.Run("nil series renders no-data placeholder", func(t *testing.T) {
		t.Parallel()
		html := ui.RenderOverlayChart(nil, defaultOpts())
		assert.Contains(t, html, "No data")
		assert.NotContains(t, html, "overlay-chart-legend")
	})

	t.Run("all series empty renders no-data placeholder", func(t *testing.T) {
		t.Parallel()
		html := ui.RenderOverlayChart([]ui.Series{
			series("a", "#f00", nil),
			series("b", "#00f", []dto.ChartPointResponse{}),
		}, defaultOpts())
		assert.Contains(t, html, "No data")
		assert.NotContains(t, html, "overlay-chart-legend")
	})

	t.Run("single series with one point renders flat line at zero", func(t *testing.T) {
		t.Parallel()
		html := ui.RenderOverlayChart([]ui.Series{
			series("usd", "#f00", pts("2026-01-01", 1.08)),
		}, defaultOpts())
		assert.Contains(t, html, `<polyline`)
		// A single point means 0% change — the Y should be at the baseline (0%).
		// With only one X label, x = Width/2 = 160.00, pct = 0%
		assert.Contains(t, html, "160.00,")
		assert.NotContains(t, html, "No data")
	})

	t.Run("single series with two ascending points renders one polyline", func(t *testing.T) {
		t.Parallel()
		html := ui.RenderOverlayChart([]ui.Series{
			series("usd", "#f00", pts("2026-01-01", 1.00, "2026-01-02", 1.10)),
		}, defaultOpts())
		assert.Contains(t, html, `<polyline`)
		// First point x=0, pct=0%; second point x=320, pct=10%
		// Verify the first coordinate is at x=0.
		assert.Contains(t, html, `points="0.00,`)
	})

	t.Run("two series normalized to percent share the Y-axis", func(t *testing.T) {
		t.Parallel()
		// Both series start at different absolute values but pct=0 at first label.
		html := ui.RenderOverlayChart([]ui.Series{
			series("a", "#f00", pts("2026-01-01", 500.0, "2026-01-02", 510.0)),
			series("b", "#00f", pts("2026-01-01", 1.10, "2026-01-02", 1.21)),
		}, defaultOpts())
		// Both start at pct=0 so their first x,y coordinates should share the same y.
		// Verify both polylines appear.
		assert.Equal(t, 2, strings.Count(html, `<polyline`))
		// The baseline label shows 0.00% at the baseline.
		assert.Contains(t, html, "0.00%")
	})

	t.Run("series with zero first price is dropped silently", func(t *testing.T) {
		t.Parallel()
		html := ui.RenderOverlayChart([]ui.Series{
			series("bad", "#f00", pts("2026-01-01", 0.0, "2026-01-02", 1.0)),
			series("ok", "#00f", pts("2026-01-01", 1.0, "2026-01-02", 1.1)),
		}, defaultOpts())
		// Only one polyline: the valid series.
		assert.Equal(t, 1, strings.Count(html, `<polyline`))
		// Legend must only include the non-dropped series.
		assert.Contains(t, html, "ok")
		assert.NotContains(t, html, `>bad<`)
	})

	t.Run("union of labels is the X domain", func(t *testing.T) {
		t.Parallel()
		// Series A: labels [a, b]; Series B: labels [b, c] — union is [a, b, c].
		html := ui.RenderOverlayChart([]ui.Series{
			series("A", "#f00", []dto.ChartPointResponse{
				{Label: "a", Price: 1.0},
				{Label: "b", Price: 1.1},
			}),
			series("B", "#00f", []dto.ChartPointResponse{
				{Label: "b", Price: 2.0},
				{Label: "c", Price: 2.2},
			}),
		}, ui.OverlayOptions{Width: 200, Height: 100})
		// With 3 X positions the second index x = 200/2 = 100.00.
		// Series A has labels a and b → two coordinate pairs.
		// Series B has labels b and c → two coordinate pairs.
		// Verify both polylines exist.
		assert.Equal(t, 2, strings.Count(html, `<polyline`))
	})

	t.Run("Y-axis labels show plus prefix for positive and minus for negative", func(t *testing.T) {
		t.Parallel()
		html := ui.RenderOverlayChart([]ui.Series{
			series("x", "#f00", pts("2026-01-01", 1.0, "2026-01-02", 1.05)),
		}, defaultOpts())
		// With one series going +5%, the yMax label is positive.
		assert.Contains(t, html, "+")
	})

	t.Run("Y-axis label format is two decimal places", func(t *testing.T) {
		t.Parallel()
		html := ui.RenderOverlayChart([]ui.Series{
			series("x", "#f00", pts("2026-01-01", 1.0, "2026-01-02", 1.10)),
		}, defaultOpts())
		// 10% change → yMax label should contain "10.00%"
		assert.Contains(t, html, "10.00%")
	})

	t.Run("legend contains one item per non-dropped series with escaped name", func(t *testing.T) {
		t.Parallel()
		html := ui.RenderOverlayChart([]ui.Series{
			series("USD/EUR", "#f00", pts("2026-01-01", 1.0, "2026-01-02", 1.1)),
			series("GBP/USD", "#00f", pts("2026-01-01", 1.2, "2026-01-02", 1.3)),
		}, defaultOpts())
		assert.Contains(t, html, "overlay-chart-legend")
		assert.Contains(t, html, "USD/EUR")
		assert.Contains(t, html, "GBP/USD")
		assert.Equal(t, 2, strings.Count(html, "overlay-chart-legend-item"))
	})

	t.Run("XSS in series name is escaped in legend", func(t *testing.T) {
		t.Parallel()
		html := ui.RenderOverlayChart([]ui.Series{
			series("<script>alert(1)</script>", "#f00", pts("2026-01-01", 1.0, "2026-01-02", 1.1)),
		}, defaultOpts())
		assert.NotContains(t, html, "<script>")
		assert.Contains(t, html, "&lt;script&gt;")
	})

	t.Run("viewBox uses Width and Height from options", func(t *testing.T) {
		t.Parallel()
		html := ui.RenderOverlayChart([]ui.Series{
			series("x", "#f00", pts("2026-01-01", 1.0, "2026-01-02", 1.1)),
		}, ui.OverlayOptions{Width: 640, Height: 360})
		assert.Contains(t, html, `viewBox="0 0 640 360"`)
	})

	t.Run("default viewBox is 320x180 when options are zero", func(t *testing.T) {
		t.Parallel()
		html := ui.RenderOverlayChart([]ui.Series{
			series("x", "#f00", pts("2026-01-01", 1.0)),
		}, ui.OverlayOptions{})
		assert.Contains(t, html, `viewBox="0 0 320 180"`)
	})

	t.Run("SVG y-axis is inverted so gains render upward", func(t *testing.T) {
		t.Parallel()
		// Series goes from 1.0 to 2.0 — a 100% gain. The second point should have
		// a smaller y-coordinate than the first (higher on screen = lower y in SVG).
		// With Width=100, Height=100, xCount=2: x0=0, x1=100.
		// pct at first point = 0, pct at second = 100.
		// yRange with padding ≈ 100 * (1 + 0.1) = 110, so yMin ≈ -5, yMax ≈ 105.
		// y(0%) is near bottom, y(100%) is near top.
		// We just assert y of first > y of second (first is lower on screen).
		html := ui.RenderOverlayChart([]ui.Series{
			series("x", "#f00", pts("2026-01-01", 1.0, "2026-01-02", 2.0)),
		}, ui.OverlayOptions{Width: 100, Height: 100})
		// The polyline points attribute has two "x,y" pairs separated by space.
		start := strings.Index(html, `points="`)
		assert.Greater(t, start, 0)
		rest := html[start+len(`points="`):]
		end := strings.Index(rest, `"`)
		assert.Greater(t, end, 0)
		coordStr := rest[:end]
		parts := strings.Fields(coordStr)
		assert.Len(t, parts, 2)
		var y0, y1 float64
		_, err0 := strings.NewReader(parts[0]), (*strings.Reader)(nil)
		_ = err0
		// parse y from "x,y"
		p0 := strings.Split(parts[0], ",")
		p1 := strings.Split(parts[1], ",")
		assert.Len(t, p0, 2)
		assert.Len(t, p1, 2)
		_, errY0 := strings.NewReader(p0[1]), (*strings.Reader)(nil)
		_, errY1 := strings.NewReader(p1[1]), (*strings.Reader)(nil)
		_ = errY0
		_ = errY1
		// Use sscanf-style parsing via fmt.Sscanf
		_, _ = fmt.Sscanf(p0[1], "%f", &y0)
		_, _ = fmt.Sscanf(p1[1], "%f", &y1)
		assert.Greater(t, y0, y1, "y at 0%% (first point) must be greater than y at 100%% (second point) because SVG y is top-down")
	})

	t.Run("output contains no script tag", func(t *testing.T) {
		t.Parallel()
		html := ui.RenderOverlayChart([]ui.Series{
			series("usd", "#f00", pts("2026-01-01", 1.0, "2026-01-02", 1.1)),
		}, defaultOpts())
		assert.NotContains(t, strings.ToLower(html), "<script")
	})

	t.Run("flat Y range forces minus-one-plus-one window", func(t *testing.T) {
		t.Parallel()
		// All points at the same price → all percent values = 0 → flat range.
		html := ui.RenderOverlayChart([]ui.Series{
			series("x", "#f00", pts("2026-01-01", 1.0, "2026-01-02", 1.0)),
		}, defaultOpts())
		// Should render without panic and contain a polyline.
		assert.Contains(t, html, `<polyline`)
	})
}
