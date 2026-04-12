package rateforecaster

import (
	"context"
	"errors"
	"fmt"

	"github.com/seilbekskindirov/monitor/internal/domain"
)

// CompositeForecaster averages the predictions of multiple Forecaster implementations.
// It requires at least one delegate to succeed; if all fail with ErrInsufficientData
// the error is propagated. Other errors from individual delegates are discarded with
// a logged warning so that partial results are still useful.
type CompositeForecaster struct {
	delegates []Forecaster
}

// NewCompositeForecaster constructs a CompositeForecaster from the provided delegates.
// Returns an error if no delegates are provided.
func NewCompositeForecaster(delegates ...Forecaster) (*CompositeForecaster, error) {
	if len(delegates) == 0 {
		return nil, errors.New("rateforecaster: CompositeForecaster requires at least one delegate")
	}
	return &CompositeForecaster{delegates: delegates}, nil
}

// Forecast returns the average of all successful delegate predictions.
// Returns ErrInsufficientData only when every delegate fails.
func (c *CompositeForecaster) Forecast(ctx context.Context, rates []*domain.RateValue) (domain.ForecastResult, error) {
	var sum float64
	var count int
	var totalDataPoints int

	for _, d := range c.delegates {
		result, err := d.Forecast(ctx, rates)
		if err != nil {
			if errors.Is(err, ErrInsufficientData) {
				continue // expected; skip silently
			}
			// Unexpected error — log and skip so remaining delegates still run.
			fmt.Printf("rateforecaster: delegate error (skipped): %v\n", err)
			continue
		}
		sum += result.PredictedPrice
		totalDataPoints += result.DataPoints
		count++
	}

	if count == 0 {
		return domain.ForecastResult{}, ErrInsufficientData
	}

	return domain.ForecastResult{
		PredictedPrice: sum / float64(count),
		Method:         "composite",
		DataPoints:     totalDataPoints / count, // average data points used across delegates
	}, nil
}

// Ensure interface is satisfied at compile time.
var _ Forecaster = (*CompositeForecaster)(nil)
