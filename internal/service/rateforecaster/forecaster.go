// Package rateforecaster provides short-term FX rate forecasting services.
package rateforecaster

import (
	"context"
	"errors"

	"github.com/seilbekskindirov/monitor/internal/domain"
)

// ErrInsufficientData is returned when there are not enough historical
// data points to produce a reliable forecast (minimum 3 required).
var ErrInsufficientData = errors.New("rateforecaster: insufficient data points (minimum 3)")

// defaultHistoryLimit is the number of historical RateValues fetched for forecasting.
const defaultHistoryLimit = 20

// Forecaster produces a short-term price prediction from a slice of historical rates.
// Implementations must be safe for concurrent use.
type Forecaster interface {
	// Forecast returns a ForecastResult or ErrInsufficientData when len(rates) < 3.
	// rates must be ordered newest-first (as returned by ObtainLastNRateValuesBySourceName).
	Forecast(ctx context.Context, rates []*domain.RateValue) (domain.ForecastResult, error)
}
