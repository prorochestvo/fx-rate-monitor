package repository

import (
	"database/sql"
	"testing"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestNewSourceRepository(t *testing.T) {
	t.Parallel()

	r, err := NewRateSourceRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestSourceRepository_Name(t *testing.T) {
	t.Parallel()

	r, err := NewRateSourceRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.Equal(t, rateSourceTableName, r.Name())
}

func TestSourceRepository_CheckUP(t *testing.T) {
	t.Parallel()

	r, err := NewRateSourceRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NoError(t, r.CheckUP(t.Context()))
}

func TestSourceRepository_RetainSource(t *testing.T) {
	t.Parallel()

	r, err := NewRateSourceRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("insert", func(t *testing.T) {
		t.Parallel()

		src := &domain.RateSource{
			Name:          "src-insert",
			Title:         "src-insert-title",
			URL:           "https://example.com/insert",
			Interval:      "5m",
			BaseCurrency:  "USD",
			QuoteCurrency: "EUR",
			Rules: []domain.RateSourceRule{
				{Method: domain.MethodRegex, Pattern: `(\d+\.\d+)`, Options: ""},
			},
		}

		require.NoError(t, r.RetainRateSource(t.Context(), src))

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM"+" "+rateSourceTableName+
				" WHERE "+rateSourceNameFieldName+" = ?",
			src.Name,
		).Scan(&count))
		require.Equal(t, 1, count)
	})
	t.Run("update", func(t *testing.T) {
		t.Parallel()

		src := &domain.RateSource{
			Name:          "src-update",
			Title:         "src-update-title",
			URL:           "https://example.com/v1",
			Interval:      "10m",
			BaseCurrency:  "USD",
			QuoteCurrency: "EUR",
			Rules:         []domain.RateSourceRule{},
		}

		require.NoError(t, r.RetainRateSource(t.Context(), src))

		src.URL = "https://example.com/v2"
		src.Interval = "1h"
		require.NoError(t, r.RetainRateSource(t.Context(), src))

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var url, interval string
		require.NoError(t, tx.QueryRow(
			"SELECT "+rateSourceURLFieldName+", "+reteSourceIntervalFieldName+
				" FROM "+rateSourceTableName+" WHERE "+rateSourceNameFieldName+" = ?",
			src.Name,
		).Scan(&url, &interval))
		require.Equal(t, "https://example.com/v2", url)
		require.Equal(t, "1h", interval)
	})
}

func TestSourceRepository_RemoveSource(t *testing.T) {
	t.Parallel()

	r, err := NewRateSourceRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		src := &domain.RateSource{
			Name:          "src-delete",
			Title:         "src-delete-title",
			URL:           "https://example.com/delete",
			Interval:      "10m",
			BaseCurrency:  "USD",
			QuoteCurrency: "KZT",
			Rules:         []domain.RateSourceRule{},
		}

		require.NoError(t, r.RetainRateSource(t.Context(), src))
		require.NoError(t, r.RemoveRateSource(t.Context(), src))

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM"+" "+rateSourceTableName+
				" WHERE "+rateSourceNameFieldName+" = ?",
			src.Name,
		).Scan(&count))
		require.Equal(t, 0, count)
	})
}

func TestSourceRepository_ObtainSourceByName(t *testing.T) {
	t.Parallel()

	r, err := NewRateSourceRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("found", func(t *testing.T) {
		t.Parallel()

		src := &domain.RateSource{
			Name:          "src-find",
			Title:         "src-find-title",
			URL:           "https://example.com/find",
			Interval:      "15m",
			BaseCurrency:  "USD",
			QuoteCurrency: "GBP",
			Rules: []domain.RateSourceRule{
				{Method: domain.MethodRegex, Pattern: `\d+`, Options: "i"},
			},
		}
		require.NoError(t, r.RetainRateSource(t.Context(), src))

		result, err := r.ObtainRateSourceByName(t.Context(), src.Name)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, src.Name, result.Name)
		require.Equal(t, src.Title, result.Title)
		require.Equal(t, src.URL, result.URL)
		require.Equal(t, src.Interval, result.Interval)
		require.Equal(t, src.BaseCurrency, result.BaseCurrency)
		require.Equal(t, src.QuoteCurrency, result.QuoteCurrency)
		require.Len(t, result.Rules, 1)
		require.Equal(t, src.Rules[0].Method, result.Rules[0].Method)
		require.Equal(t, src.Rules[0].Pattern, result.Rules[0].Pattern)
	})
	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		result, err := r.ObtainRateSourceByName(t.Context(), "nonexistent")
		require.NoError(t, err)
		require.Nil(t, result)
	})
}

func TestSourceRepository_ObtainAllSources(t *testing.T) {
	t.Parallel()

	r, err := NewRateSourceRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("returns sources", func(t *testing.T) {
		t.Parallel()

		sources := []domain.RateSource{
			{Name: "src-all-1", URL: "https://example.com/1", Interval: "5m", BaseCurrency: "USD", QuoteCurrency: "EUR", Rules: []domain.RateSourceRule{}},
			{Name: "src-all-2", URL: "https://example.com/2", Interval: "10m", BaseCurrency: "USD", QuoteCurrency: "KZT", Rules: []domain.RateSourceRule{}},
		}
		for _, source := range sources {
			require.NoError(t, r.RetainRateSource(t.Context(), &source))
		}

		result, err := r.ObtainAllRateSources(t.Context())
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(result), 2)
	})
	t.Run("empty", func(t *testing.T) {
		t.Parallel()

		emptyRepo, err := NewRateSourceRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		result, err := emptyRepo.ObtainAllRateSources(t.Context())
		require.NoError(t, err)
		require.Empty(t, result)
	})
}
