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
