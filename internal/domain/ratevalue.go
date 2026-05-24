package domain

import "time"

// RateValue represents a single exchange rate data point.
type RateValue struct {
	ID            string
	SourceName    string
	BaseCurrency  string
	QuoteCurrency string
	Price         float64
	Timestamp     time.Time
}

// ForecastResult holds the output of a short-term rate forecast.
type ForecastResult struct {
	// PredictedPrice is the estimated next rate value.
	PredictedPrice float64
	// Method describes the algorithm used (e.g. "composite", "moving_average", "linear_regression").
	Method string
	// DataPoints is the number of historical values used to produce the forecast.
	DataPoints int
}

// ChartPeriod specifies the time window for aggregated rate chart data.
type ChartPeriod string

const (
	// ChartPeriodWeek aggregates rate data over the past 7 days, grouped by day.
	ChartPeriodWeek ChartPeriod = "week"
	// ChartPeriodMonth aggregates rate data over the past 30 days, grouped by day.
	ChartPeriodMonth ChartPeriod = "month"
	// ChartPeriodYear aggregates rate data over the past 12 months, grouped by month.
	ChartPeriodYear ChartPeriod = "year"
)

// ChartPoint is one aggregated data point on a rate chart.
type ChartPoint struct {
	Label string  // "2026-04-03" (week/month) or "2026-04" (year)
	Price float64 // AVG(price) for the bucket
}
