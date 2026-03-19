package rateforecaster

import (
	"context"

	"github.com/seilbekskindirov/monitor/internal/domain"
)

// MovingAverageForecaster predicts the next rate as the arithmetic mean
// of the most recent N historical prices.
type MovingAverageForecaster struct{}

// NewMovingAverageForecaster returns a new MovingAverageForecaster.
func NewMovingAverageForecaster() *MovingAverageForecaster {
	return &MovingAverageForecaster{}
}

// Forecast returns the arithmetic mean of all provided rate prices.
// rates must be ordered newest-first. Returns ErrInsufficientData when len(rates) < 3.
func (f *MovingAverageForecaster) Forecast(_ context.Context, rates []*domain.RateValue) (domain.ForecastResult, error) {
	if len(rates) < 3 {
		return domain.ForecastResult{}, ErrInsufficientData
	}

	var sum float64
	for _, r := range rates {
		sum += r.Price
	}

	return domain.ForecastResult{
		PredictedPrice: sum / float64(len(rates)),
		Method:         "moving_average",
		DataPoints:     len(rates),
	}, nil
}

// Ensure interface is satisfied at compile time.
var _ Forecaster = (*MovingAverageForecaster)(nil)
