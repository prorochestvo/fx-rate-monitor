package repository

import (
	"database/sql"
	"sort"
	"testing"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestNewRateRepository(t *testing.T) {
	t.Parallel()

	r, err := NewRateValueRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestRateRepository_Name(t *testing.T) {
	t.Parallel()

	r, err := NewRateValueRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.Equal(t, rateValueTableName, r.Name())
}

func TestRateRepository_CheckUP(t *testing.T) {
	t.Parallel()

	r, err := NewRateValueRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NoError(t, r.CheckUP(t.Context()))
}

func TestRateRepository_RetainRateValue(t *testing.T) {
	t.Parallel()

	r, err := NewRateValueRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("insert", func(t *testing.T) {
		t.Parallel()

		rate := &domain.RateValue{
			SourceName:    "test-source",
			BaseCurrency:  "USD",
			QuoteCurrency: "EUR",
			Price:         1.23,
		}
		require.Empty(t, rate.ID)

		err = r.RetainRateValue(t.Context(), rate)
		require.NoError(t, err)
		require.NotEmpty(t, rate.ID)
		require.False(t, rate.Timestamp.IsZero())

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		require.NotNil(t, tx)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow("SELECT COUNT(*) FROM"+" "+rateValueTableName+" WHERE "+rateValueIdFieldName+" = ?", rate.ID).Scan(&count))
		require.Equal(t, 1, count)
	})
	t.Run("update", func(t *testing.T) {
		t.Parallel()

		rate := &domain.RateValue{
			SourceName:    "test-source",
			BaseCurrency:  "USD",
			QuoteCurrency: "RUS",
			Price:         1.23,
		}
		require.Empty(t, rate.ID)

		require.NoError(t, r.RetainRateValue(t.Context(), rate))
		require.NotEmpty(t, rate.ID)

		rate.Price = 9.99
		id := rate.ID

		require.NoError(t, r.RetainRateValue(t.Context(), rate))
		require.NotEmpty(t, rate.ID)
		require.Equal(t, id, rate.ID)

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		require.NotNil(t, tx)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow("SELECT COUNT(*) FROM"+" "+rateValueTableName+" WHERE "+rateValueIdFieldName+" = ?", rate.ID).Scan(&count))
		require.NoError(t, err)
		require.Equal(t, 1, count)

		var value float64
		require.NoError(t, tx.QueryRow("SELECT "+rateValuePriceFieldName+" FROM"+" "+rateValueTableName+" WHERE "+rateValueIdFieldName+" = ?", rate.ID).Scan(&value))
		require.NoError(t, err)
		require.Equal(t, 9.99, value)
	})
}

func TestRateRepository_RemoveRateValue(t *testing.T) {
	t.Parallel()

	r, err := NewRateValueRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		rate := &domain.RateValue{
			SourceName:    "test-source",
			BaseCurrency:  "USD",
			QuoteCurrency: "EUR",
			Price:         1.23,
		}
		require.Empty(t, rate.ID)

		err = r.RetainRateValue(t.Context(), rate)
		require.NoError(t, err)
		require.NotEmpty(t, rate.ID)
		require.False(t, rate.Timestamp.IsZero())

		err = r.RemoveRateValue(t.Context(), rate)
		require.NoError(t, err)

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		require.NotNil(t, tx)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow("SELECT COUNT(*) FROM"+" "+rateValueTableName+" WHERE "+rateValueIdFieldName+" = ?", rate.ID).Scan(&count))
		require.Equal(t, 0, count)
	})
}

func TestRateRepository_ObtainAllRateValueBySourceName(t *testing.T) {
	t.Parallel()

	r, err := NewRateValueRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("returns rates for source", func(t *testing.T) {
		t.Parallel()

		rates := []domain.RateValue{
			{SourceName: "src-a", BaseCurrency: "USD", QuoteCurrency: "EUR", Price: 1.1},
			{SourceName: "src-a", BaseCurrency: "USD", QuoteCurrency: "GBP", Price: 1.2},
			{SourceName: "src-b", BaseCurrency: "USD", QuoteCurrency: "JPY", Price: 150},
		}
		for _, rate := range rates {
			require.NoError(t, r.RetainRateValue(t.Context(), &rate))
		}

		result, err := r.ObtainAllRateValueBySourceName(t.Context(), "src-a")
		require.NoError(t, err)
		require.Len(t, result, 2)

		sort.Slice(result, func(i, j int) bool {
			return result[i].QuoteCurrency < result[j].QuoteCurrency
		})

		require.Equal(t, result[0].Price, 1.1)
		require.Equal(t, result[1].Price, 1.2)
	})
	t.Run("empty for unknown source", func(t *testing.T) {
		t.Parallel()

		result, err := r.ObtainAllRateValueBySourceName(t.Context(), "nonexistent")
		require.NoError(t, err)
		require.Empty(t, result)
	})
}

func TestRateRepository_ObtainLastNRateValuesBySourceName(t *testing.T) {
	t.Parallel()

	r, err := NewRateValueRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("zero rows", func(t *testing.T) {
		t.Parallel()

		result, err := r.ObtainLastNRateValuesBySourceName(t.Context(), "empty-source", 2)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Empty(t, result)
	})
	t.Run("fewer rows than limit", func(t *testing.T) {
		t.Parallel()

		for _, price := range []float64{1.0, 2.0} {
			rate := &domain.RateValue{SourceName: "few-source", BaseCurrency: "USD", QuoteCurrency: "EUR", Price: price}
			require.NoError(t, r.RetainRateValue(t.Context(), rate))
		}

		result, err := r.ObtainLastNRateValuesBySourceName(t.Context(), "few-source", 5)
		require.NoError(t, err)
		require.Len(t, result, 2)
	})
	t.Run("more rows than limit returns newest first", func(t *testing.T) {
		t.Parallel()

		for i := 0; i < 5; i++ {
			rate := &domain.RateValue{SourceName: "many-source", BaseCurrency: "USD", QuoteCurrency: "KZT", Price: float64(100 + i)}
			require.NoError(t, r.RetainRateValue(t.Context(), rate))
		}

		result, err := r.ObtainLastNRateValuesBySourceName(t.Context(), "many-source", 2)
		require.NoError(t, err)
		require.Len(t, result, 2)
		// Newest first: index 0 must have the highest price (inserted last)
		require.GreaterOrEqual(t, result[0].Price, result[1].Price)
	})
}

func TestRateValueRepository_ObtainRateValueChartBySourceName(t *testing.T) {
	t.Parallel()

	seedRates := func(t *testing.T, r *RateValueRepository, sourceName string, prices []float64, timestamps []time.Time) {
		t.Helper()
		for i, price := range prices {
			rv := &domain.RateValue{
				SourceName:    sourceName,
				BaseCurrency:  "USD",
				QuoteCurrency: "KZT",
				Price:         price,
			}
			require.NoError(t, r.RetainRateValue(t.Context(), rv))
			// overwrite timestamp via direct SQL — RetainRateValue sets it to now
			sqliteDB := r.db
			tx, err := sqliteDB.Transaction(t.Context())
			require.NoError(t, err)
			_, err = tx.ExecContext(t.Context(),
				"UPDATE "+rateValueTableName+" SET "+rateValueTimestampFieldName+" = ? WHERE "+rateValueIdFieldName+" = ?",
				timestamps[i].Format(time.RFC3339), rv.ID,
			)
			require.NoError(t, err)
			require.NoError(t, tx.Commit())
		}
	}

	t.Run("week period returns daily buckets within last 7 days", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateValueRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		now := time.Now().UTC()
		prices := []float64{450.0, 451.0, 452.0}
		ts := []time.Time{
			now.AddDate(0, 0, -3),
			now.AddDate(0, 0, -2),
			now.AddDate(0, 0, -1),
		}
		seedRates(t, r, "chart-src", prices, ts)

		points, err := r.ObtainRateValueChartBySourceName(t.Context(), "chart-src", ChartPeriodWeek)
		require.NoError(t, err)
		require.NotEmpty(t, points)
		require.LessOrEqual(t, len(points), 7)
		// labels must be in YYYY-MM-DD format
		require.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, points[0].Label)
	})

	t.Run("year period returns monthly buckets in YYYY-MM format", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateValueRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		now := time.Now().UTC()
		prices := []float64{460.0, 462.0}
		ts := []time.Time{
			now.AddDate(0, -2, 0),
			now.AddDate(0, -1, 0),
		}
		seedRates(t, r, "chart-year-src", prices, ts)

		points, err := r.ObtainRateValueChartBySourceName(t.Context(), "chart-year-src", ChartPeriodYear)
		require.NoError(t, err)
		require.NotEmpty(t, points)
		require.LessOrEqual(t, len(points), 12)
		require.Regexp(t, `^\d{4}-\d{2}$`, points[0].Label)
	})

	t.Run("unknown period returns error", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateValueRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		_, err = r.ObtainRateValueChartBySourceName(t.Context(), "any", ChartPeriod("bogus"))
		require.Error(t, err)
	})

	t.Run("no data returns empty slice without error", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateValueRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		points, err := r.ObtainRateValueChartBySourceName(t.Context(), "empty-source", ChartPeriodWeek)
		require.NoError(t, err)
		require.Empty(t, points)
	})
}
