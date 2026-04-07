package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateUserSubscriptionValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		sub       RateUserSubscription
		wantError bool
	}{
		{
			name:      "delta valid",
			sub:       RateUserSubscription{ConditionType: ConditionTypeDelta, ConditionValue: "10"},
			wantError: false,
		},
		{
			name:      "daily_8am valid",
			sub:       RateUserSubscription{ConditionType: ConditionTypeDaily, ConditionValue: "08:00:00"},
			wantError: false,
		},
		{
			name:      "daily_6pm valid",
			sub:       RateUserSubscription{ConditionType: ConditionTypeDaily, ConditionValue: "18:00:00"},
			wantError: false,
		},
		{
			name:      "interval 1m valid (minimum boundary)",
			sub:       RateUserSubscription{ConditionType: ConditionTypeInterval, ConditionValue: time.Minute.String()},
			wantError: false,
		},
		{
			name:      "interval 6h valid",
			sub:       RateUserSubscription{ConditionType: ConditionTypeInterval, ConditionValue: (6 * time.Hour).String()},
			wantError: false,
		},
		{
			name:      "interval 24h valid",
			sub:       RateUserSubscription{ConditionType: ConditionTypeInterval, ConditionValue: (24 * time.Hour).String()},
			wantError: false,
		},
		{
			name:      "interval zero invalid",
			sub:       RateUserSubscription{ConditionType: ConditionTypeInterval, ConditionValue: "0"},
			wantError: true,
		},
		{
			name:      "interval 30s invalid (below minimum)",
			sub:       RateUserSubscription{ConditionType: ConditionTypeInterval, ConditionValue: (30 * time.Second).String()},
			wantError: true,
		},
		{
			name:      "interval negative invalid",
			sub:       RateUserSubscription{ConditionType: ConditionTypeInterval, ConditionValue: (-5 * time.Minute).String()},
			wantError: true,
		},
		{
			name:      "unknown condition type invalid",
			sub:       RateUserSubscription{ConditionType: "bogus"},
			wantError: true,
		},
		{
			name:      "empty condition type invalid",
			sub:       RateUserSubscription{ConditionType: ""},
			wantError: true,
		},
		{
			name:      "cron condition every 10 minutes invalid",
			sub:       RateUserSubscription{ConditionType: ConditionTypeCron, ConditionValue: "*/10 * * * *"},
			wantError: false,
		},
		{
			name:      "cron condition every day at midnight invalid",
			sub:       RateUserSubscription{ConditionType: ConditionTypeCron, ConditionValue: "0 0 * * *"},
			wantError: false,
		},
		{
			name:      "cron condition every Monday at 9am invalid",
			sub:       RateUserSubscription{ConditionType: ConditionTypeCron, ConditionValue: "0 9 * * 1"},
			wantError: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.sub.Validate()
			if tc.wantError {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
