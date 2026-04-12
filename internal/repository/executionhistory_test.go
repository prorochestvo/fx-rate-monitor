package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestNewExecutionHistoryRepository(t *testing.T) {
	t.Parallel()

	r, err := NewExecutionHistoryRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestExecutionHistoryRepository_Name(t *testing.T) {
	t.Parallel()

	r, err := NewExecutionHistoryRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.Equal(t, executionHistoryTableName, r.Name())
}

func TestExecutionHistoryRepository_CheckUP(t *testing.T) {
	t.Parallel()

	r, err := NewExecutionHistoryRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NoError(t, r.CheckUP(t.Context()))
}

func TestExecutionHistoryRepository_RetainAndObtain(t *testing.T) {
	t.Parallel()

	r, err := NewExecutionHistoryRepository(stubSQLiteDB(t))
	require.NoError(t, err)

	t.Run("nil record returns error", func(t *testing.T) {
		t.Parallel()

		err := r.RetainExecutionHistory(t.Context(), nil)
		require.Error(t, err)
		require.ErrorContains(t, err, "nil")
	})
	t.Run("insert success record", func(t *testing.T) {
		t.Parallel()

		h := &domain.ExecutionHistory{
			SourceName: "halyk_bank",
			Success:    true,
			Timestamp:  time.Now().UTC().Truncate(time.Second),
		}
		require.NoError(t, r.RetainExecutionHistory(t.Context(), h))
		require.NotEmpty(t, h.ID)
	})
	t.Run("insert failure record", func(t *testing.T) {
		t.Parallel()

		h := &domain.ExecutionHistory{
			SourceName: "kaspi_bank",
			Success:    false,
			Error:      "connection refused",
			Timestamp:  time.Now().UTC().Truncate(time.Second),
		}
		require.NoError(t, r.RetainExecutionHistory(t.Context(), h))
		require.NotEmpty(t, h.ID)
	})
}

func TestExecutionHistoryRepository_ObtainLastN(t *testing.T) {
	t.Parallel()

	r, err := NewExecutionHistoryRepository(stubSQLiteDB(t))
	require.NoError(t, err)

	t.Run("zero rows returns empty non-nil slice", func(t *testing.T) {
		t.Parallel()

		records, err := r.ObtainLastNExecutionHistoryBySourceName(t.Context(), "nonexistent", 5, false)
		require.NoError(t, err)
		require.NotNil(t, records)
		require.Empty(t, records)
	})
	t.Run("successOnly filters failures", func(t *testing.T) {
		t.Parallel()

		src := "filtered-source"
		now := time.Now().UTC()

		rows := []domain.ExecutionHistory{
			{SourceName: src, Success: true, Timestamp: now.Add(-2 * time.Minute)},
			{SourceName: src, Success: false, Error: "oops", Timestamp: now.Add(-time.Minute)},
			{SourceName: src, Success: true, Timestamp: now},
		}
		for _, row := range rows {
			require.NoError(t, r.RetainExecutionHistory(t.Context(), &row))
		}

		result, err := r.ObtainLastNExecutionHistoryBySourceName(t.Context(), src, 10, true)
		require.NoError(t, err)
		require.Len(t, result, 2, "only successful rows")
		for _, rec := range result {
			require.True(t, rec.Success)
		}
	})
	t.Run("successOnly=false returns all rows", func(t *testing.T) {
		t.Parallel()

		src := "all-rows-source"
		now := time.Now().UTC()

		rows := []domain.ExecutionHistory{
			{SourceName: src, Success: true, Timestamp: now.Add(-2 * time.Minute)},
			{SourceName: src, Success: false, Error: "err", Timestamp: now.Add(-time.Minute)},
			{SourceName: src, Success: true, Timestamp: now},
		}
		for _, row := range rows {
			require.NoError(t, r.RetainExecutionHistory(t.Context(), &row))
		}

		result, err := r.ObtainLastNExecutionHistoryBySourceName(t.Context(), src, 10, false)
		require.NoError(t, err)
		require.Len(t, result, 3)
	})
	t.Run("limit is respected newest-first", func(t *testing.T) {
		t.Parallel()

		src := "limit-source"
		now := time.Now().UTC()

		for i := 0; i < 5; i++ {
			h := &domain.ExecutionHistory{
				SourceName: src,
				Success:    true,
				Timestamp:  now.Add(time.Duration(i) * time.Minute),
			}
			require.NoError(t, r.RetainExecutionHistory(t.Context(), h))
		}

		result, err := r.ObtainLastNExecutionHistoryBySourceName(t.Context(), src, 2, false)
		require.NoError(t, err)
		require.Len(t, result, 2)
		// newest-first: result[0].Timestamp >= result[1].Timestamp
		require.True(t, !result[0].Timestamp.Before(result[1].Timestamp))
	})
}

func TestExecutionHistoryRepository_RemoveSourceExecutionHistory(t *testing.T) {
	t.Parallel()

	r, err := NewExecutionHistoryRepository(stubSQLiteDB(t))
	require.NoError(t, err)

	t.Run("nil record returns error", func(t *testing.T) {
		t.Parallel()

		err := r.RemoveSourceExecutionHistory(t.Context(), nil)
		require.Error(t, err)
		require.ErrorContains(t, err, "nil")
	})

	h := &domain.ExecutionHistory{
		SourceName: "to-remove",
		Success:    true,
		Timestamp:  time.Now().UTC(),
	}
	require.NoError(t, r.RetainExecutionHistory(t.Context(), h))
	require.NotEmpty(t, h.ID)

	require.NoError(t, r.RemoveSourceExecutionHistory(t.Context(), h))

	tx, err := r.db.Transaction(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	var count int
	require.NoError(t, tx.QueryRow("SELECT COUNT(*) FROM"+" "+executionHistoryTableName+" WHERE "+executionHistoryIDFieldName+" = ?", h.ID).Scan(&count))
	require.Equal(t, 0, count)
}

func TestExecutionHistoryRepository_TransactionErrors(t *testing.T) {
	t.Parallel()

	newBrokenRepo := func(t *testing.T) *ExecutionHistoryRepository {
		t.Helper()
		r, err := NewExecutionHistoryRepository(stubSQLiteDB(t))
		require.NoError(t, err)
		r.db = &mockFailDB{err: errors.New("db unavailable")}
		return r
	}

	t.Run("CheckUP propagates transaction error", func(t *testing.T) {
		t.Parallel()
		require.Error(t, newBrokenRepo(t).CheckUP(t.Context()))
	})
	t.Run("ObtainLastNExecutionHistoryBySourceName propagates transaction error", func(t *testing.T) {
		t.Parallel()
		_, err := newBrokenRepo(t).ObtainLastNExecutionHistoryBySourceName(t.Context(), "src", 1, false)
		require.Error(t, err)
	})
	t.Run("RetainExecutionHistory propagates transaction error", func(t *testing.T) {
		t.Parallel()
		err := newBrokenRepo(t).RetainExecutionHistory(t.Context(), &domain.ExecutionHistory{SourceName: "src"})
		require.Error(t, err)
	})
	t.Run("RemoveSourceExecutionHistory propagates transaction error", func(t *testing.T) {
		t.Parallel()
		err := newBrokenRepo(t).RemoveSourceExecutionHistory(t.Context(), &domain.ExecutionHistory{ID: "x"})
		require.Error(t, err)
	})
}

func BenchmarkExecutionHistoryRepository_ObtainLastN(b *testing.B) {
	r, err := NewExecutionHistoryRepository(stubSQLiteDB(b))
	if err != nil {
		b.Fatal(err)
	}

	ctx := b.Context()
	src := "bench-source"
	now := time.Now().UTC()

	for i := 0; i < 200; i++ {
		h := &domain.ExecutionHistory{
			SourceName: src,
			Success:    i%2 == 0,
			Timestamp:  now.Add(time.Duration(i) * time.Second),
		}
		if err := r.RetainExecutionHistory(ctx, h); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = r.ObtainLastNExecutionHistoryBySourceName(ctx, src, 10, true)
	}
}
