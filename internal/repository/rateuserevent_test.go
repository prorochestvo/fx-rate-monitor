package repository

import (
	"database/sql"
	"testing"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/stretchr/testify/require"
	"github.com/twinj/uuid"
)

func TestNewRateUserEventRepository(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserEventRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestRateUserEventRepository_Name(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserEventRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.Equal(t, rateUserEventTableName, r.Name())
}

func TestRateUserEventRepository_CheckUP(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserEventRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NoError(t, r.CheckUP(t.Context()))
}

func TestRateUserEventRepository_ObtainLastNRateUserEvents(t *testing.T) {
	t.Parallel()

	t.Run("no status filter returns all rows", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		for _, status := range []domain.RateUserEventStatus{
			domain.RateUserEventStatusPending,
			domain.RateUserEventStatusSent,
			domain.RateUserEventStatusFailed,
		} {
			require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
				UserType: domain.UserTypeTelegram,
				UserID:   uuid.NewV4().String(),
				Message:  "msg",
				Status:   status,
			}))
		}

		result, err := r.ObtainLastNRateUserEvents(t.Context(), 0, 10)
		require.NoError(t, err)
		require.Len(t, result, 3)
	})

	t.Run("status filter returns only matching rows", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		for i := 0; i < 2; i++ {
			require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
				UserType: domain.UserTypeTelegram,
				UserID:   uuid.NewV4().String(),
				Message:  "msg",
				Status:   domain.RateUserEventStatusPending,
			}))
		}
		require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
			UserType: domain.UserTypeTelegram,
			UserID:   uuid.NewV4().String(),
			Message:  "msg",
			Status:   domain.RateUserEventStatusSent,
		}))

		result, err := r.ObtainLastNRateUserEvents(t.Context(), 0, 10, domain.RateUserEventStatusPending)
		require.NoError(t, err)
		require.Len(t, result, 2)
	})

	t.Run("offset and limit are applied", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		base := time.Now().UTC().Truncate(time.Second)
		for i := 0; i < 5; i++ {
			require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
				UserType:  domain.UserTypeTelegram,
				UserID:    uuid.NewV4().String(),
				Message:   "msg",
				Status:    domain.RateUserEventStatusPending,
				CreatedAt: base.Add(time.Duration(i) * time.Second),
			}))
		}

		result, err := r.ObtainLastNRateUserEvents(t.Context(), 2, 2)
		require.NoError(t, err)
		require.Len(t, result, 2)
		require.Equal(t, base.Add(2*time.Second).Format(time.RFC3339), result[0].CreatedAt.Format(time.RFC3339))
		require.Equal(t, base.Add(3*time.Second).Format(time.RFC3339), result[1].CreatedAt.Format(time.RFC3339))
	})

	t.Run("empty table returns non-nil empty slice", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		result, err := r.ObtainLastNRateUserEvents(t.Context(), 0, 10)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result, 0)
	})

	t.Run("rows are ordered oldest first", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		base := time.Now().UTC().Truncate(time.Second)
		for i := 0; i < 3; i++ {
			require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
				UserType:  domain.UserTypeTelegram,
				UserID:    uuid.NewV4().String(),
				Message:   "msg",
				Status:    domain.RateUserEventStatusPending,
				CreatedAt: base.Add(time.Duration(i) * time.Second),
			}))
		}

		result, err := r.ObtainLastNRateUserEvents(t.Context(), 0, 10)
		require.NoError(t, err)
		require.Len(t, result, 3)
		require.True(t, result[0].CreatedAt.Before(result[1].CreatedAt))
	})
}

func TestRateUserEventRepository_ObtainUnprocessedRateUserEvents(t *testing.T) {
	t.Parallel()

	t.Run("returns pending and failed only", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		for _, status := range []domain.RateUserEventStatus{
			domain.RateUserEventStatusPending,
			domain.RateUserEventStatusFailed,
			domain.RateUserEventStatusSent,
			domain.RateUserEventStatusCanceled,
		} {
			require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
				UserType: domain.UserTypeTelegram,
				UserID:   uuid.NewV4().String(),
				Message:  "msg",
				Status:   status,
			}))
		}

		result, err := r.ObtainUnprocessedRateUserEvents(t.Context())
		require.NoError(t, err)
		require.Len(t, result, 2)
	})

	t.Run("empty when all events are sent", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		for i := 0; i < 2; i++ {
			require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
				UserType: domain.UserTypeTelegram,
				UserID:   uuid.NewV4().String(),
				Message:  "msg",
				Status:   domain.RateUserEventStatusSent,
			}))
		}

		result, err := r.ObtainUnprocessedRateUserEvents(t.Context())
		require.NoError(t, err)
		require.Len(t, result, 0)
	})

	t.Run("empty table", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		result, err := r.ObtainUnprocessedRateUserEvents(t.Context())
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result, 0)
	})

	t.Run("ordered oldest first", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		base := time.Now().UTC().Truncate(time.Second)
		require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
			UserType:  domain.UserTypeTelegram,
			UserID:    uuid.NewV4().String(),
			Message:   "failed event",
			Status:    domain.RateUserEventStatusFailed,
			CreatedAt: base.Add(-2 * time.Second),
		}))
		require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
			UserType:  domain.UserTypeTelegram,
			UserID:    uuid.NewV4().String(),
			Message:   "pending event",
			Status:    domain.RateUserEventStatusPending,
			CreatedAt: base.Add(-1 * time.Second),
		}))

		result, err := r.ObtainUnprocessedRateUserEvents(t.Context())
		require.NoError(t, err)
		require.Len(t, result, 2)
		require.Equal(t, domain.RateUserEventStatusFailed, result[0].Status)
	})
}

func TestRateUserEventRepository_ObtainRateUserEventById(t *testing.T) {
	t.Parallel()

	t.Run("found — all fields match", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		event := &domain.RateUserEvent{
			UserType: domain.UserTypeTelegram,
			UserID:   uuid.NewV4().String(),
			Message:  "hello",
			Status:   domain.RateUserEventStatusPending,
		}
		require.NoError(t, r.RetainRateUserEvent(t.Context(), event))

		result, err := r.ObtainRateUserEventById(t.Context(), event.ID)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, event.UserType, result.UserType)
		require.Equal(t, event.UserID, result.UserID)
		require.Equal(t, event.Message, result.Message)
		require.Equal(t, event.Status, result.Status)
		require.Equal(t, event.CreatedAt.Format(time.RFC3339), result.CreatedAt.Format(time.RFC3339))
	})

	t.Run("not found returns nil without error", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		result, err := r.ObtainRateUserEventById(t.Context(), "nonexistent-id")
		require.NoError(t, err)
		require.Nil(t, result)
	})

	t.Run("SentAt nil round-trips as zero time", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		event := &domain.RateUserEvent{
			UserType: domain.UserTypeTelegram,
			UserID:   uuid.NewV4().String(),
			Message:  "msg",
			Status:   domain.RateUserEventStatusPending,
		}
		require.NoError(t, r.RetainRateUserEvent(t.Context(), event))

		result, err := r.ObtainRateUserEventById(t.Context(), event.ID)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.True(t, result.SentAt.IsZero())
	})

	t.Run("SentAt non-nil round-trips", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		sentAt := time.Now().UTC().Truncate(time.Second)
		event := &domain.RateUserEvent{
			UserType: domain.UserTypeTelegram,
			UserID:   uuid.NewV4().String(),
			Message:  "msg",
			Status:   domain.RateUserEventStatusSent,
			SentAt:   sentAt,
		}
		require.NoError(t, r.RetainRateUserEvent(t.Context(), event))

		result, err := r.ObtainRateUserEventById(t.Context(), event.ID)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.False(t, result.SentAt.IsZero())
		require.Equal(t, sentAt.Format(time.RFC3339), result.SentAt.Format(time.RFC3339))
	})
}

func TestRateUserEventRepository_RetainRateUserEventRepository(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserEventRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("insert", func(t *testing.T) {
		t.Parallel()

		rue := &domain.RateUserEvent{
			UserType: "user-3",
			UserID:   uuid.NewV4().String(),
			Message:  "test message",
			Status:   domain.RateUserEventStatusPending,
		}
		require.Empty(t, rue.ID)
		require.True(t, rue.CreatedAt.IsZero())
		require.True(t, rue.SentAt.IsZero())

		require.NoError(t, r.RetainRateUserEvent(t.Context(), rue))
		require.NotEmpty(t, rue.ID)
		require.True(t, rue.SentAt.IsZero())
		require.False(t, rue.CreatedAt.IsZero())

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM"+" "+rateUserEventTableName+
				" WHERE "+rateUserEventUserTypeFieldName+" = ?"+
				" AND "+rateUserEventUserIDFieldName+" = ?",
			rue.UserType, rue.UserID,
		).Scan(&count))
		require.Equal(t, 1, count)
		require.NoError(t, tx.QueryRow("SELECT COUNT(*) FROM"+" "+rateUserEventTableName+" WHERE "+rateUserEventIDFieldName+" = ?", rue.ID).Scan(&count))
		require.Equal(t, 1, count)
	})
	t.Run("idempotent", func(t *testing.T) {
		t.Parallel()

		rue := &domain.RateUserEvent{
			UserType: "user-3",
			UserID:   uuid.NewV4().String(),
			Message:  "test message",
			Status:   domain.RateUserEventStatusPending,
		}

		require.NoError(t, r.RetainRateUserEvent(t.Context(), rue))
		newId := rue.ID
		require.NoError(t, r.RetainRateUserEvent(t.Context(), rue))
		require.Equal(t, newId, rue.ID)
		require.NoError(t, r.RetainRateUserEvent(t.Context(), rue))
		require.Equal(t, newId, rue.ID)

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM"+" "+rateUserEventTableName+
				" WHERE "+rateUserEventUserTypeFieldName+" = ?"+
				" AND "+rateUserEventUserIDFieldName+" = ?",
			rue.UserType, rue.UserID,
		).Scan(&count))
		require.Equal(t, 1, count)
		require.NoError(t, tx.QueryRow("SELECT COUNT(*) FROM"+" "+rateUserEventTableName+" WHERE "+rateUserEventIDFieldName+" = ?", rue.ID).Scan(&count))
		require.Equal(t, 1, count)
	})
	t.Run("send time round-trips", func(t *testing.T) {
		t.Parallel()

		rue := &domain.RateUserEvent{
			UserType: "user-3",
			UserID:   uuid.NewV4().String(),
			Message:  "test message",
			Status:   domain.RateUserEventStatusPending,
		}
		require.True(t, rue.SentAt.IsZero())
		require.NoError(t, r.RetainRateUserEvent(t.Context(), rue))
		require.True(t, rue.SentAt.IsZero())

		result, err := r.ObtainRateUserEventById(t.Context(), rue.ID)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.True(t, result.SentAt.IsZero())

		sentAt := time.Now().UTC()

		rue.SentAt = sentAt
		require.NoError(t, r.RetainRateUserEvent(t.Context(), rue))
		require.False(t, rue.SentAt.IsZero(), rue.SentAt.Format(time.RFC3339))

		result, err = r.ObtainRateUserEventById(t.Context(), rue.ID)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.False(t, result.SentAt.IsZero())
		require.Equal(t, sentAt.Format(time.RFC3339), result.SentAt.Format(time.RFC3339))
	})
}

func TestRateUserEventRepository_RemoveRateUserEventRepository(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserEventRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		rue := &domain.RateUserEvent{
			UserType: "user-3",
			UserID:   uuid.NewV4().String(),
			Message:  "test message",
			Status:   domain.RateUserEventStatusPending,
		}

		require.NoError(t, r.RetainRateUserEvent(t.Context(), rue))
		require.NoError(t, r.RemoveRateUserEvent(t.Context(), rue))

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM"+" "+rateUserEventTableName+
				" WHERE "+rateUserEventUserTypeFieldName+" = ?"+
				" AND "+rateUserEventUserIDFieldName+" = ?",
			rue.UserType, rue.UserID,
		).Scan(&count))
		require.Equal(t, 0, count)
		require.NoError(t, tx.QueryRow("SELECT COUNT(*) FROM"+" "+rateUserEventTableName+" WHERE "+rateUserEventIDFieldName+" = ?", rue.ID).Scan(&count))
		require.Equal(t, 0, count)
	})
}

func TestRateUserEventRepository_SourceNameRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("source_name persists and is read back correctly", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		event := &domain.RateUserEvent{
			SourceName: "KAZ_NATIONALBANK_USD_KZT",
			UserType:   domain.UserTypeTelegram,
			UserID:     "user-42",
			Message:    "rate changed",
			Status:     domain.RateUserEventStatusPending,
		}
		require.NoError(t, r.RetainRateUserEvent(t.Context(), event))
		require.NotEmpty(t, event.ID)

		result, err := r.ObtainRateUserEventById(t.Context(), event.ID)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, "KAZ_NATIONALBANK_USD_KZT", result.SourceName)
	})

	t.Run("existing rows without source_name have empty string", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		// Insert without setting SourceName — should default to ""
		event := &domain.RateUserEvent{
			UserType: domain.UserTypeTelegram,
			UserID:   "user-99",
			Message:  "old event",
			Status:   domain.RateUserEventStatusSent,
		}
		require.NoError(t, r.RetainRateUserEvent(t.Context(), event))

		result, err := r.ObtainRateUserEventById(t.Context(), event.ID)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, "", result.SourceName)
	})
}

func TestRateUserEventRepository_ObtainRateUserEventsBySourceName(t *testing.T) {
	t.Parallel()

	t.Run("returns only events for the given source", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
			SourceName: "src-A",
			UserType:   domain.UserTypeTelegram,
			UserID:     "111",
			Message:    "msg",
			Status:     domain.RateUserEventStatusFailed,
		}))
		require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
			SourceName: "src-B",
			UserType:   domain.UserTypeTelegram,
			UserID:     "222",
			Message:    "msg",
			Status:     domain.RateUserEventStatusFailed,
		}))

		result, err := r.ObtainRateUserEventsBySourceName(t.Context(), "src-A", 0, 10)
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Equal(t, "src-A", result[0].SourceName)
	})

	t.Run("status filter works correctly", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		for _, status := range []domain.RateUserEventStatus{
			domain.RateUserEventStatusFailed,
			domain.RateUserEventStatusSent,
			domain.RateUserEventStatusPending,
		} {
			require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
				SourceName: "src-X",
				UserType:   domain.UserTypeTelegram,
				UserID:     "u1",
				Message:    "msg",
				Status:     status,
			}))
		}

		failed, err := r.ObtainRateUserEventsBySourceName(t.Context(), "src-X", 0, 10, domain.RateUserEventStatusFailed)
		require.NoError(t, err)
		require.Len(t, failed, 1)
		require.Equal(t, domain.RateUserEventStatusFailed, failed[0].Status)
	})

	t.Run("no status args returns all statuses for source", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		for _, status := range []domain.RateUserEventStatus{
			domain.RateUserEventStatusFailed,
			domain.RateUserEventStatusSent,
		} {
			require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
				SourceName: "src-Y",
				UserType:   domain.UserTypeTelegram,
				UserID:     "u2",
				Message:    "msg",
				Status:     status,
			}))
		}

		all, err := r.ObtainRateUserEventsBySourceName(t.Context(), "src-Y", 0, 10)
		require.NoError(t, err)
		require.Len(t, all, 2)
	})

	t.Run("offset and limit paginate correctly", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		for i := 0; i < 5; i++ {
			require.NoError(t, r.RetainRateUserEvent(t.Context(), &domain.RateUserEvent{
				SourceName: "src-Z",
				UserType:   domain.UserTypeTelegram,
				UserID:     "u3",
				Message:    "msg",
				Status:     domain.RateUserEventStatusFailed,
			}))
		}

		page1, err := r.ObtainRateUserEventsBySourceName(t.Context(), "src-Z", 0, 3)
		require.NoError(t, err)
		require.Len(t, page1, 3)

		page2, err := r.ObtainRateUserEventsBySourceName(t.Context(), "src-Z", 3, 3)
		require.NoError(t, err)
		require.Len(t, page2, 2)
	})

	t.Run("unknown source returns empty slice", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		result, err := r.ObtainRateUserEventsBySourceName(t.Context(), "nonexistent", 0, 10)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Empty(t, result)
	})
}

func TestRateUserEventRepository_RemoveRateUserEventOlderThan(t *testing.T) {
	t.Parallel()

	t.Run("removes old non-pending event", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		event := &domain.RateUserEvent{
			UserType:  domain.UserTypeTelegram,
			UserID:    uuid.NewV4().String(),
			Message:   "old sent",
			Status:    domain.RateUserEventStatusSent,
			CreatedAt: time.Now().UTC().Add(-48 * time.Hour),
		}
		require.NoError(t, r.RetainRateUserEvent(t.Context(), event))

		require.NoError(t, r.RemoveRateUserEventOlderThan(t.Context(), 24*time.Hour))

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM "+rateUserEventTableName+" WHERE "+rateUserEventIDFieldName+" = ?",
			event.ID,
		).Scan(&count))
		require.Equal(t, 0, count)
	})

	t.Run("does not remove old pending event", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		event := &domain.RateUserEvent{
			UserType:  domain.UserTypeTelegram,
			UserID:    uuid.NewV4().String(),
			Message:   "old pending",
			Status:    domain.RateUserEventStatusPending,
			CreatedAt: time.Now().UTC().Add(-48 * time.Hour),
		}
		require.NoError(t, r.RetainRateUserEvent(t.Context(), event))

		require.NoError(t, r.RemoveRateUserEventOlderThan(t.Context(), 24*time.Hour))

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM "+rateUserEventTableName+" WHERE "+rateUserEventIDFieldName+" = ?",
			event.ID,
		).Scan(&count))
		require.Equal(t, 1, count)
	})

	t.Run("does not remove recent non-pending event", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		event := &domain.RateUserEvent{
			UserType: domain.UserTypeTelegram,
			UserID:   uuid.NewV4().String(),
			Message:  "recent sent",
			Status:   domain.RateUserEventStatusSent,
		}
		require.NoError(t, r.RetainRateUserEvent(t.Context(), event))

		require.NoError(t, r.RemoveRateUserEventOlderThan(t.Context(), 24*time.Hour))

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM "+rateUserEventTableName+" WHERE "+rateUserEventIDFieldName+" = ?",
			event.ID,
		).Scan(&count))
		require.Equal(t, 1, count)
	})

	t.Run("negative duration is treated as positive", func(t *testing.T) {
		t.Parallel()

		r, err := NewRateUserEventRepository(stubSQLiteDB(t))
		require.NoError(t, err)

		event := &domain.RateUserEvent{
			UserType:  domain.UserTypeTelegram,
			UserID:    uuid.NewV4().String(),
			Message:   "old sent neg",
			Status:    domain.RateUserEventStatusSent,
			CreatedAt: time.Now().UTC().Add(-48 * time.Hour),
		}
		require.NoError(t, r.RetainRateUserEvent(t.Context(), event))

		require.NoError(t, r.RemoveRateUserEventOlderThan(t.Context(), -24*time.Hour))

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM "+rateUserEventTableName+" WHERE "+rateUserEventIDFieldName+" = ?",
			event.ID,
		).Scan(&count))
		require.Equal(t, 0, count)
	})
}
