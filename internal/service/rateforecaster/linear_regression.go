package rateforecaster

import (
	"context"
	"math"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"gonum.org/v1/gonum/stat"
)

// LinearRegressionForecaster fits a least-squares line price = alpha + beta*t
// over historical timestamps and extrapolates one average interval into the future.
type LinearRegressionForecaster struct{}

// NewLinearRegressionForecaster returns a new LinearRegressionForecaster.
func NewLinearRegressionForecaster() *LinearRegressionForecaster {
	return &LinearRegressionForecaster{}
}

// Forecast fits a linear model to the provided rate history and predicts the next price.
// rates must be ordered newest-first. Returns ErrInsufficientData when len(rates) < 3.
//
// Edge case: when all timestamps are identical (e.g. rapid same-second collection),
// Var(x) == 0 so gonum cannot determine a slope and returns NaN. In that case the
// forecaster falls back to the arithmetic mean of the prices, which is the best
// estimate available given indistinguishable time positions.
func (f *LinearRegressionForecaster) Forecast(_ context.Context, rates []*domain.RateValue) (domain.ForecastResult, error) {
	if len(rates) < 3 {
		return domain.ForecastResult{}, ErrInsufficientData
	}

	n := len(rates)
	xs := make([]float64, n)
	ys := make([]float64, n)

	// rates is newest-first; reverse so xs is monotonically increasing (oldest → newest)
	// as required by gonum's LinearRegression.
	for i, r := range rates {
		xs[n-1-i] = float64(r.Timestamp.Unix())
		ys[n-1-i] = r.Price
	}

	alpha, beta := stat.LinearRegression(xs, ys, nil, false)

	avgInterval := (xs[n-1] - xs[0]) / float64(n-1)
	tNext := xs[n-1] + avgInterval
	predicted := alpha + beta*tNext

	// Guard: when all xs are identical Var(x)=0 → beta=NaN → predicted=NaN.
	// Fall back to the arithmetic mean of ys, which is the optimal constant predictor.
	if math.IsNaN(predicted) {
		var sum float64
		for _, y := range ys {
			sum += y
		}
		predicted = sum / float64(n)
	}

	return domain.ForecastResult{
		PredictedPrice: predicted,
		Method:         "linear_regression",
		DataPoints:     n,
	}, nil
}

// Ensure interface is satisfied at compile time.
var _ Forecaster = (*LinearRegressionForecaster)(nil)
