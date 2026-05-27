package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/seilbekskindirov/monitor/cmd/wasm/dom"
	"github.com/seilbekskindirov/monitor/internal/dto"
)

// Series is one currency-pair line in an overlay chart. Color is a CSS color
// string from the curated palette in application/me_subscriptions.go — it is
// not user-controlled and is interpolated into the legend swatch style without
// HTML escaping (the palette contains only ASCII hex/keyword colors, safe to
// embed verbatim in style attributes).
type Series struct {
	Name   string
	Color  string
	Points []dto.ChartPointResponse
}

// OverlayOptions controls the SVG viewBox dimensions. Width and Height define
// the coordinate space only; actual on-screen size is governed by the CSS
// rules attached to .overlay-chart in subscriptions.html.
type OverlayOptions struct {
	Width  int
	Height int
}

const (
	defaultOverlayWidth  = 320
	defaultOverlayHeight = 180
)

// RenderOverlayChart returns the HTML for one full-width overlay chart with a
// legend below. Every series is normalized to percent change from its own first
// point so currency pairs at wildly different absolute scales share a common
// Y-axis. Edge cases:
//   - nil or all-empty series → "No data" placeholder, no legend.
//   - single-point series     → flat line at 0% across the full X-range.
//   - series with first price == 0 → dropped silently from chart and legend.
//
// The output is a self-contained HTML fragment safe for innerHTML assignment.
func RenderOverlayChart(series []Series, opts OverlayOptions) string {
	if opts.Width <= 0 {
		opts.Width = defaultOverlayWidth
	}
	if opts.Height <= 0 {
		opts.Height = defaultOverlayHeight
	}

	// Build the union of X-axis labels in first-occurrence order, deduplicated.
	// We also compute normalized percent series while iterating.
	type normalizedSeries struct {
		name   string
		color  string
		pctPts map[string]float64 // label → percent-change value
	}

	allLabelsOrdered := make([]string, 0)
	labelSeen := make(map[string]bool)

	var active []normalizedSeries

	for _, s := range series {
		if len(s.Points) == 0 {
			continue
		}
		first := s.Points[0].Price
		if first == 0 {
			// Zero first price makes normalization undefined. Drop silently.
			continue
		}

		ns := normalizedSeries{
			name:   s.Name,
			color:  s.Color,
			pctPts: make(map[string]float64, len(s.Points)),
		}

		for _, pt := range s.Points {
			pct := (pt.Price - first) / first * 100
			ns.pctPts[pt.Label] = pct

			if !labelSeen[pt.Label] {
				labelSeen[pt.Label] = true
				allLabelsOrdered = append(allLabelsOrdered, pt.Label)
			}
		}
		active = append(active, ns)
	}

	if len(active) == 0 {
		return renderOverlayNoData(opts)
	}

	// Compute global Y-axis range across all plotted percent values plus 0
	// (baseline must always be in the visible range).
	yMin, yMax := 0.0, 0.0
	for _, ns := range active {
		for _, v := range ns.pctPts {
			if v < yMin {
				yMin = v
			}
			if v > yMax {
				yMax = v
			}
		}
	}

	yRange := yMax - yMin
	if yRange == 0 {
		// All series are flat at 0% — force a small window so the baseline is centered.
		yMin = -1
		yMax = 1
		yRange = 2
	}

	// 5% padding on each end so lines don't touch the chart edges.
	pad := yRange * 0.05
	yMin -= pad
	yMax += pad
	yRange = yMax - yMin

	w := float64(opts.Width)
	h := float64(opts.Height)

	xCount := len(allLabelsOrdered)
	xOf := func(idx int) float64 {
		if xCount <= 1 {
			return w / 2
		}
		return float64(idx) * w / float64(xCount-1)
	}

	// SVG y-axis is top-down: higher percent values map to smaller y coordinates.
	yOf := func(pct float64) float64 {
		return h - (pct-yMin)/yRange*h
	}

	// Build a label→index lookup for fast coordinate computation.
	labelIndex := make(map[string]int, xCount)
	for i, lbl := range allLabelsOrdered {
		labelIndex[lbl] = i
	}

	var b strings.Builder
	b.WriteString(`<div class="overlay-chart-wrap">`)
	fmt.Fprintf(&b, `<svg class="overlay-chart" viewBox="0 0 %d %d" preserveAspectRatio="none">`,
		opts.Width, opts.Height)

	// Baseline at y(0%).
	baseline := strconv.FormatFloat(yOf(0), 'f', 2, 64)
	fmt.Fprintf(&b, `<line class="overlay-chart-baseline" x1="0" y1="%s" x2="%d" y2="%s"/>`,
		baseline, opts.Width, baseline)

	// Y-axis min/max labels.
	yMaxStr := formatPct(yMax - pad) // un-pad to show the real data extremes
	yMinStr := formatPct(yMin + pad)
	fmt.Fprintf(&b, `<text class="overlay-chart-axis-label" x="2" y="12">%s</text>`,
		dom.Escape(yMaxStr))
	fmt.Fprintf(&b, `<text class="overlay-chart-axis-label" x="2" y="%s">%s</text>`,
		strconv.FormatFloat(h-2, 'f', 2, 64), dom.Escape(yMinStr))

	// One polyline per active series.
	for _, ns := range active {
		var coords []string
		for i, lbl := range allLabelsOrdered {
			v, ok := ns.pctPts[lbl]
			if !ok {
				// This series has no point at this label — skip (gap in line).
				continue
			}
			_ = labelIndex[lbl] // suppress unused-variable lint hint
			x := strconv.FormatFloat(xOf(i), 'f', 2, 64)
			y := strconv.FormatFloat(yOf(v), 'f', 2, 64)
			coords = append(coords, x+","+y)
		}
		// Color comes from the curated palette — ASCII hex only, no escaping needed.
		// Escaping would mangle '#' in some implementations; the palette is not
		// user-controlled so we interpolate directly.
		fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="1.5" points="%s"/>`,
			ns.color, strings.Join(coords, " "))
	}

	b.WriteString(`</svg>`)

	// Legend.
	b.WriteString(`<div class="overlay-chart-legend">`)
	for _, ns := range active {
		b.WriteString(`<span class="overlay-chart-legend-item">`)
		// Color swatch: palette color in inline style (not user-controlled, no escaping needed).
		fmt.Fprintf(&b, `<span class="overlay-chart-legend-swatch" style="background:%s"></span>`,
			ns.color)
		b.WriteString(dom.Escape(ns.name))
		b.WriteString(`</span>`)
	}
	b.WriteString(`</div>`)

	b.WriteString(`</div>`)
	return b.String()
}

// formatPct formats a percent value with two decimal places and a leading +
// for positive values.
func formatPct(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	if v > 0 {
		s = "+" + s
	}
	return s + "%"
}

// renderOverlayNoData returns the no-data placeholder HTML for the overlay chart.
func renderOverlayNoData(opts OverlayOptions) string {
	var b strings.Builder
	b.WriteString(`<div class="overlay-chart-wrap">`)
	fmt.Fprintf(&b, `<svg class="overlay-chart" viewBox="0 0 %d %d" preserveAspectRatio="none">`,
		opts.Width, opts.Height)
	fmt.Fprintf(&b, `<text class="overlay-chart-axis-label" x="%s" y="%s" text-anchor="middle">No data</text>`,
		strconv.FormatFloat(float64(opts.Width)/2, 'f', 2, 64),
		strconv.FormatFloat(float64(opts.Height)/2, 'f', 2, 64))
	b.WriteString(`</svg>`)
	b.WriteString(`</div>`)
	return b.String()
}
