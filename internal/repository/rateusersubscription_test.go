package repository

import (
	"database/sql"
	"testing"
	"time"

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
	require.Equal(t, rateUserSubscriptionTableName, r.Name())
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
			SourceName:     "src-a",
			ConditionType:  domain.ConditionTypeDelta,
			ConditionValue: "0.5",
		}

		require.NoError(t, r.RetainRateUserSubscription(t.Context(), sub))
		require.False(t, sub.CreatedAt.IsZero())

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM"+" "+rateUserSubscriptionTableName+
				" WHERE "+rateUserSubscriptionUserTypeFieldName+" = ?"+
				" AND "+rateUserSubscriptionUserIDFieldName+" = ?"+
				" AND "+rateUserSubscriptionSourceNameFieldName+" = ?",
			sub.UserType, sub.UserID, sub.SourceName,
		).Scan(&count))
		require.Equal(t, 1, count)
	})
	t.Run("idempotent", func(t *testing.T) {
		t.Parallel()

		sub := &domain.RateUserSubscription{
			UserType:   domain.UserTypeTelegram,
			UserID:     "user-2",
			SourceName: "src-b",
		}

		require.NoError(t, r.RetainRateUserSubscription(t.Context(), sub))
		require.NoError(t, r.RetainRateUserSubscription(t.Context(), sub))

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM"+" "+rateUserSubscriptionTableName+
				" WHERE "+rateUserSubscriptionUserTypeFieldName+" = ?"+
				" AND "+rateUserSubscriptionUserIDFieldName+" = ?"+
				" AND "+rateUserSubscriptionSourceNameFieldName+" = ?",
			sub.UserType, sub.UserID, sub.SourceName,
		).Scan(&count))
		require.Equal(t, 1, count)
	})
	t.Run("delta_threshold round-trips", func(t *testing.T) {
		t.Parallel()

		sub := &domain.RateUserSubscription{
			UserType:       domain.UserTypeTelegram,
			UserID:         "user-dt",
			SourceName:     "src-dt",
			ConditionType:  domain.ConditionTypeDelta,
			ConditionValue: "0.75",
		}
		require.NoError(t, r.RetainRateUserSubscription(t.Context(), sub))

		result, err := r.ObtainRateUserSubscriptionsBySource(t.Context(), "src-dt")
		require.NoError(t, err)
		require.Len(t, result, 1)

		deltaThreshold, err := result[0].DeltaThreshold()
		require.NoError(t, err)
		require.Equal(t, 0.75, deltaThreshold)
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
			UserType:   domain.UserTypeTelegram,
			UserID:     "user-3",
			SourceName: "src-c",
		}

		require.NoError(t, r.RetainRateUserSubscription(t.Context(), sub))
		require.NoError(t, r.RemoveRateUserSubscription(t.Context(), sub))

		tx, err := r.db.Transaction(t.Context())
		require.NoError(t, err)
		defer func(tx *sql.Tx) { require.NoError(t, tx.Rollback()) }(tx)

		var count int
		require.NoError(t, tx.QueryRow(
			"SELECT COUNT(*) FROM"+" "+rateUserSubscriptionTableName+
				" WHERE "+rateUserSubscriptionUserTypeFieldName+" = ?"+
				" AND "+rateUserSubscriptionUserIDFieldName+" = ?"+
				" AND "+rateUserSubscriptionSourceNameFieldName+" = ?",
			sub.UserType, sub.UserID, sub.SourceName,
		).Scan(&count))
		require.Equal(t, 0, count)
	})
}

func TestRateUserSubscriptionRepository_ObtainRateUserSubscriptionsByUserID(t *testing.T) {
	t.Parallel()

	r, err := NewRateUserSubscriptionRepository(stubSQLiteDB(t))
	require.NoError(t, err)
	require.NotNil(t, r)

	now := time.Now().UTC()

	t.Run("returns subscriptions for user", func(t *testing.T) {
		t.Parallel()

		subs := []domain.RateUserSubscription{
			{UserType: domain.UserTypeTelegram, UserID: "user-4", SourceName: "src-a", ConditionType: domain.ConditionTypeDelta, ConditionValue: "10", LatestNotifiedRate: 11.13, CreatedAt: now},
			{UserType: domain.UserTypeTelegram, UserID: "user-4", SourceName: "src-b", ConditionType: domain.ConditionTypeInterval, ConditionValue: "10h", LatestNotifiedRate: 12.12, CreatedAt: now},
			{UserType: domain.UserTypeTelegram, UserID: "user-4", SourceName: "src-c", ConditionType: domain.ConditionTypeDaily, ConditionValue: "10:00:00", LatestNotifiedRate: 13.11, CreatedAt: now},
			{UserType: domain.UserTypeTelegram, UserID: "user-5", SourceName: "src-a", ConditionType: domain.ConditionTypeCron, ConditionValue: "*/5 * * *", LatestNotifiedRate: 14.21, CreatedAt: now},
		}
		for i := range subs {
			require.NoError(t, r.RetainRateUserSubscription(t.Context(), &subs[i]))
		}

		result, err := r.ObtainRateUserSubscriptionsByUserID(t.Context(), domain.UserTypeTelegram, "user-4")
		require.NoError(t, err)
		require.Len(t, result, 3)
		for i, r := range result {
			require.Equal(t, domain.UserTypeTelegram, r.UserType)
			require.Equal(t, "user-4", r.UserID)
			require.Equal(t, subs[i].UserType, r.UserType)
			require.Equal(t, subs[i].UserID, r.UserID)
			require.Equal(t, subs[i].SourceName, r.SourceName)
			require.Equal(t, subs[i].ConditionType, r.ConditionType)
			require.Equal(t, subs[i].ConditionValue, r.ConditionValue)
			require.Equal(t, subs[i].LatestNotifiedRate, r.LatestNotifiedRate)
			require.Equal(t, subs[i].CreatedAt.Format(time.RFC3339), r.CreatedAt.Format(time.RFC3339))
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
			{UserType: domain.UserTypeTelegram, UserID: "user-6", SourceName: "src-x"},
			{UserType: domain.UserTypeTelegram, UserID: "user-7", SourceName: "src-x"},
			{UserType: domain.UserTypeTelegram, UserID: "user-8", SourceName: "src-y"},
		}
		for i := range subs {
			require.NoError(t, r.RetainRateUserSubscription(t.Context(), &subs[i]))
		}

		result, err := r.ObtainRateUserSubscriptionsBySource(t.Context(), "src-x")
		require.NoError(t, err)
		require.Len(t, result, 2)
		for _, s := range result {
			require.Equal(t, "src-x", s.SourceName)
		}
	})
	t.Run("empty for unknown source", func(t *testing.T) {
		t.Parallel()

		result, err := r.ObtainRateUserSubscriptionsBySource(t.Context(), "nonexistent")
		require.NoError(t, err)
		require.Empty(t, result)
	})
}
