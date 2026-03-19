package repository

import (
	"database/sql"
	"testing"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestNewRateUserSubscriptionRepository(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserSubscriptionRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)
}

func TestRateUserSubscriptionRepository_Name(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserSubscriptionRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.Equal(t, subscriptionTableName, r.Name())
}

func TestRateUserSubscriptionRepository_CheckUP(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserSubscriptionRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NoError(t, r.CheckUP(t.Context()))
}

func TestRateUserSubscriptionRepository_RetainRateUserSubscription(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserSubscriptionRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("insert", func(t *testing.T) {
		t.Parallel()

		sub := &domain.RateUserSubscription{
			UserType:       domain.UserTypeTelegram,
			UserID:         "user-1",
			Source:         "src-a",
			DeltaThreshold: 0.5,
		}

		require.NoError(t, r.RetainRateUserSubscription(t.Context(), sub))
		require.False(t, sub.CreatedAt.IsZero())

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM"+" "+subscriptionTableName+
				" WHERE "+subscriptionUserTypeFieldName+" = ?"+
				" AND "+subscriptionUserIDFieldName+" = ?"+
				" AND "+subscriptionSourceNameFieldName+" = ?",
			sub.UserType, sub.UserID, sub.Source,
		).Scan(&count))
		require.Equal(t, 1, count)
	})
	t.Run("idempotent", func(t *testing.T) {
		t.Parallel()

		sub := &domain.RateUserSubscription{
			UserType: domain.UserTypeTelegram,
			UserID:   "user-2",
			Source:   "src-b",
		}

		require.NoError(t, r.RetainRateUserSubscription(t.Context(), sub))
		require.NoError(t, r.RetainRateUserSubscription(t.Context(), sub))

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM"+" "+subscriptionTableName+
				" WHERE "+subscriptionUserTypeFieldName+" = ?"+
				" AND "+subscriptionUserIDFieldName+" = ?"+
				" AND "+subscriptionSourceNameFieldName+" = ?",
			sub.UserType, sub.UserID, sub.Source,
		).Scan(&count))
		require.Equal(t, 1, count)
	})
	t.Run("delta_threshold round-trips", func(t *testing.T) {
		t.Parallel()

		sub := &domain.RateUserSubscription{
			UserType:       domain.UserTypeTelegram,
			UserID:         "user-dt",
			Source:         "src-dt",
			DeltaThreshold: 0.75,
		}
		require.NoError(t, r.RetainRateUserSubscription(t.Context(), sub))

		result, err := r.ObtainRateUserSubscriptionsBySource(t.Context(), "src-dt")
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Equal(t, 0.75, result[0].DeltaThreshold)
	})
}

func TestRateUserSubscriptionRepository_RemoveRateUserSubscription(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserSubscriptionRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		sub := &domain.RateUserSubscription{
			UserType: domain.UserTypeTelegram,
			UserID:   "user-3",
			Source:   "src-c",
		}

		require.NoError(t, r.RetainRateUserSubscription(t.Context(), sub))
		require.NoError(t, r.RemoveRateUserSubscription(t.Context(), sub))

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM"+" "+subscriptionTableName+
				" WHERE "+subscriptionUserTypeFieldName+" = ?"+
				" AND "+subscriptionUserIDFieldName+" = ?"+
				" AND "+subscriptionSourceNameFieldName+" = ?",
			sub.UserType, sub.UserID, sub.Source,
		).Scan(&count))
		require.Equal(t, 0, count)
	})
}

func TestRateUserSubscriptionRepository_ObtainRateUserSubscriptionsByUserID(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserSubscriptionRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("returns subscriptions for user", func(t *testing.T) {
		t.Parallel()

		subs := []domain.RateUserSubscription{
			{UserType: domain.UserTypeTelegram, UserID: "user-4", Source: "src-a"},
			{UserType: domain.UserTypeTelegram, UserID: "user-4", Source: "src-b"},
			{UserType: domain.UserTypeTelegram, UserID: "user-5", Source: "src-a"},
		}
		for i := range subs {
			require.NoError(t, r.RetainRateUserSubscription(t.Context(), &subs[i]))
		}

		result, err := r.ObtainRateUserSubscriptionsByUserID(t.Context(), domain.UserTypeTelegram, "user-4")
		require.NoError(t, err)
		require.Len(t, result, 2)
		for _, s := range result {
			require.Equal(t, domain.UserTypeTelegram, s.UserType)
			require.Equal(t, "user-4", s.UserID)
		}
	})
	t.Run("empty for unknown user", func(t *testing.T) {
		t.Parallel()

		result, err := r.ObtainRateUserSubscriptionsByUserID(t.Context(), domain.UserTypeTelegram, "nonexistent")
		require.NoError(t, err)
		require.Empty(t, result)
	})
}

func TestRateUserSubscriptionRepository_ObtainRateUserSubscriptionsBySource(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserSubscriptionRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	t.Run("returns subscriptions for source", func(t *testing.T) {
		t.Parallel()

		subs := []domain.RateUserSubscription{
			{UserType: domain.UserTypeTelegram, UserID: "user-6", Source: "src-x"},
			{UserType: domain.UserTypeTelegram, UserID: "user-7", Source: "src-x"},
			{UserType: domain.UserTypeTelegram, UserID: "user-8", Source: "src-y"},
		}
		for i := range subs {
			require.NoError(t, r.RetainRateUserSubscription(t.Context(), &subs[i]))
		}

		result, err := r.ObtainRateUserSubscriptionsBySource(t.Context(), "src-x")
		require.NoError(t, err)
		require.Len(t, result, 2)
		for _, s := range result {
			require.Equal(t, "src-x", s.Source)
		}
	})
	t.Run("empty for unknown source", func(t *testing.T) {
		t.Parallel()

		result, err := r.ObtainRateUserSubscriptionsBySource(t.Context(), "nonexistent")
		require.NoError(t, err)
		require.Empty(t, result)
	})
}
