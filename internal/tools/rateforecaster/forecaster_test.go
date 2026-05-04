package rateforecaster

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeRates builds a slice of RateValues ordered newest-first, with each entry
// separated by one minute. This mirrors what ObtainLastNRateValuesBySourceName returns.
func makeRates(tb testing.TB, prices []float64) []*domain.RateValue {
	tb.Helper()
	now := time.Now().UTC()
	n := len(prices)
	rates := make([]*domain.RateValue, n)
	for i, p := range prices {
		rates[i] = &domain.RateValue{
			Price:     p,
			Timestamp: now.Add(-time.Duration(i) * time.Minute),
		}
	}
	return rates
}

func TestMovingAverageForecaster(t *testing.T) {
	t.Parallel()

	f := NewMovingAverageForecaster()

	t.Run("happy path — returns arithmetic mean", func(t *testing.T) {
		t.Parallel()

		// prices: [5, 4, 3, 2, 1] (newest-first), mean = 3.0
		rates := makeRates(t, []float64{5, 4, 3, 2, 1})
		result, err := f.Forecast(t.Context(), rates)
		require.NoError(t, err)
		assert.Equal(t, 3.0, result.PredictedPrice)
		assert.Equal(t, "moving_average", result.Method)
		assert.Equal(t, 5, result.DataPoints)
	})

	t.Run("single price repeated — returns same price", func(t *testing.T) {
		t.Parallel()

		rates := makeRates(t, []float64{100, 100, 100})
		result, err := f.Forecast(t.Context(), rates)
		require.NoError(t, err)
		assert.Equal(t, 100.0, result.PredictedPrice)
	})

	t.Run("insufficient data — exactly 2 rates", func(t *testing.T) {
		t.Parallel()

		rates := makeRates(t, []float64{1, 2})
		_, err := f.Forecast(t.Context(), rates)
		require.ErrorIs(t, err, ErrInsufficientData)
	})

	t.Run("insufficient data — empty slice", func(t *testing.T) {
		t.Parallel()

		_, err := f.Forecast(t.Context(), nil)
		require.ErrorIs(t, err, ErrInsufficientData)
	})
}

func TestLinearRegressionForecaster(t *testing.T) {
	t.Parallel()

	f := NewLinearRegressionForecaster()

	t.Run("happy path — perfectly linear prices predict next value", func(t *testing.T) {
		t.Parallel()

		// Build 5 rates with a perfect linear trend: prices increase by 1 per minute.
		// Newest-first: [504, 503, 502, 501, 500] → oldest price=500 at t-4m, newest=504 at t.
		// The regression should extrapolate one interval (1 min) forward to predict ≈505.
		base := time.Now().UTC()
		n := 5
		rates := make([]*domain.RateValue, n)
		for i := 0; i < n; i++ {
			rates[i] = &domain.RateValue{
				Price:     float64(504 - i), // newest-first: 504, 503, ..., 500
				Timestamp: base.Add(-time.Duration(i) * time.Minute),
			}
		}

		result, err := f.Forecast(t.Context(), rates)
		require.NoError(t, err)
		assert.InDelta(t, 505.0, result.PredictedPrice, 0.5)
		assert.Equal(t, "linear_regression", result.Method)
		assert.Equal(t, 5, result.DataPoints)
	})

	t.Run("insufficient data — 1 rate", func(t *testing.T) {
		t.Parallel()

		rates := makeRates(t, []float64{500})
		_, err := f.Forecast(t.Context(), rates)
		require.ErrorIs(t, err, ErrInsufficientData)
	})

	t.Run("insufficient data — exactly 2 rates", func(t *testing.T) {
		t.Parallel()

		rates := makeRates(t, []float64{502, 500})
		_, err := f.Forecast(t.Context(), rates)
		require.ErrorIs(t, err, ErrInsufficientData)
	})

	t.Run("identical timestamps — falls back to arithmetic mean", func(t *testing.T) {
		t.Parallel()

		// When all xs are equal, Var(x)=0 → gonum returns NaN for beta.
		// The forecaster must detect NaN and fall back to the mean of prices.
		// Input newest-first [503, 502, 501] → reversed ys [501, 502, 503] → mean = 502.
		now := time.Now().UTC()
		rates := []*domain.RateValue{
			{Price: 503, Timestamp: now},
			{Price: 502, Timestamp: now},
			{Price: 501, Timestamp: now},
		}

		result, err := f.Forecast(t.Context(), rates)
		require.NoError(t, err)
		assert.Equal(t, "linear_regression", result.Method)
		assert.Equal(t, 3, result.DataPoints)
		assert.InDelta(t, 502.0, result.PredictedPrice, 1e-9, "identical timestamps: expected mean fallback")
		assert.False(t, math.IsNaN(result.PredictedPrice), "predicted price must not be NaN")
	})
}

func TestCompositeForecaster(t *testing.T) {
	t.Parallel()

	t.Run("empty delegates — constructor error", func(t *testing.T) {
		t.Parallel()

		_, err := NewCompositeForecaster()
		require.Error(t, err)
	})

	t.Run("all delegates succeed — returns average prediction", func(t *testing.T) {
		t.Parallel()

		// Build 5 evenly-spaced rates for deterministic MA and LR results.
		base := time.Now().UTC()
		rates := make([]*domain.RateValue, 5)
		for i := 0; i < 5; i++ {
			rates[i] = &domain.RateValue{
				Price:     float64(504 - i),
				Timestamp: base.Add(-time.Duration(i) * time.Minute),
			}
		}

		ma := NewMovingAverageForecaster()
		lr := NewLinearRegressionForecaster()
		comp, err := NewCompositeForecaster(ma, lr)
		require.NoError(t, err)

		maResult, err := ma.Forecast(t.Context(), rates)
		require.NoError(t, err)
		lrResult, err := lr.Forecast(t.Context(), rates)
		require.NoError(t, err)
		expected := (maResult.PredictedPrice + lrResult.PredictedPrice) / 2

		result, fErr := comp.Forecast(t.Context(), rates)
		require.NoError(t, fErr)
		assert.InDelta(t, expected, result.PredictedPrice, 1e-9)
		assert.Equal(t, "composite", result.Method)
	})

	t.Run("all delegates fail — propagates ErrInsufficientData", func(t *testing.T) {
		t.Parallel()

		rates := makeRates(t, []float64{500}) // only 1 point → all delegates fail

		comp, err := NewCompositeForecaster(
			NewMovingAverageForecaster(),
			NewLinearRegressionForecaster(),
		)
		require.NoError(t, err)

		_, fErr := comp.Forecast(t.Context(), rates)
		require.ErrorIs(t, fErr, ErrInsufficientData)
	})

	t.Run("one delegate fails unexpectedly, one succeeds — returns successful prediction", func(t *testing.T) {
		t.Parallel()

		rates := makeRates(t, []float64{5, 4, 3, 2, 1})

		comp, err := NewCompositeForecaster(
			NewMovingAverageForecaster(),
			&alwaysErrForecaster{err: errors.New("unexpected failure")},
		)
		require.NoError(t, err)

		// MA mean of [5,4,3,2,1] = 3.0; alwaysErrForecaster is skipped with warning.
		result, fErr := comp.Forecast(t.Context(), rates)
		require.NoError(t, fErr)
		assert.InDelta(t, 3.0, result.PredictedPrice, 1e-9)
		assert.Equal(t, "composite", result.Method)
	})
}

// BenchmarkMovingAverageForecaster measures throughput at the default history size.
func BenchmarkMovingAverageForecaster(b *testing.B) {
	f := NewMovingAverageForecaster()
	prices := make([]float64, defaultHistoryLimit)
	for i := range prices {
		prices[i] = float64(500 + i)
	}
	rates := makeRates(b, prices)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = f.Forecast(b.Context(), rates)
	}
}

// BenchmarkLinearRegressionForecaster measures throughput at the default history size.
func BenchmarkLinearRegressionForecaster(b *testing.B) {
	f := NewLinearRegressionForecaster()
	prices := make([]float64, defaultHistoryLimit)
	for i := range prices {
		prices[i] = float64(500 + i)
	}
	rates := makeRates(b, prices)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = f.Forecast(b.Context(), rates)
	}
}

// alwaysErrForecaster is a Forecaster that always returns a configurable error.
// Used to verify CompositeForecaster skips unexpected (non-ErrInsufficientData) errors.
type alwaysErrForecaster struct {
	err error
}

func (a *alwaysErrForecaster) Forecast(_ context.Context, _ []*domain.RateValue) (domain.ForecastResult, error) {
	return domain.ForecastResult{}, a.err
}
