package domain

import (
	"fmt"
	"strconv"
	"time"

	"github.com/robfig/cron/v3"
)

// RateUserSubscription represents a user's subscription to a monitored rate source.
type RateUserSubscription struct {
	UserType           UserType                  `json:"user_type"`
	UserID             string                    `json:"user_id"`
	SourceName         string                    `json:"source_name"`
	ConditionType      SubscriptionConditionType `json:"condition_type"`
	ConditionValue     string                    `json:"condition_value"`
	LatestNotifiedRate float64                   `json:"latest_notified_rate"`
	UpdatedAt          time.Time                 `json:"updated_at"`
	CreatedAt          time.Time                 `json:"created_at"`
}

// Validate returns a non-nil error if the subscription is misconfigured.
func (rus *RateUserSubscription) Validate() error {
	switch rus.ConditionType {
	case ConditionTypeDaily:
		_, err := rus.DailyTime()
		return err
	case ConditionTypeDelta:
		_, err := rus.DeltaThreshold()
		return err
	case ConditionTypeInterval:
		_, err := rus.IntervalDuration()
		return err
	case ConditionTypeCron:
		_, err := rus.CronDuration()
		return err
	default:
		return fmt.Errorf("unknown condition type: %q", rus.ConditionType)
	}
}

func (rus *RateUserSubscription) DailyTime() (time.Time, error) {
	if rus.ConditionType != ConditionTypeDaily {
		return time.Time{}, fmt.Errorf("invalid condition type: %s", rus.ConditionType)
	}
	return time.Parse(time.TimeOnly, rus.ConditionValue)
}

func (rus *RateUserSubscription) DeltaThreshold() (float64, error) {
	if rus.ConditionType != ConditionTypeDelta {
		return 0, fmt.Errorf("invalid condition type: %s", rus.ConditionType)
	}
	d, err := strconv.ParseFloat(rus.ConditionValue, 10)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("invalid interval: %s", rus.ConditionValue)
	}
	return d, nil
}

func (rus *RateUserSubscription) CronDuration() (time.Duration, error) {
	if rus.ConditionType != ConditionTypeCron {
		return 0, fmt.Errorf("invalid condition type: %s", rus.ConditionType)
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

	schedule, err := parser.Parse(rus.ConditionValue)
	if err != nil {
		return 0, err
	}

	now := time.Now().UTC()
	next := schedule.Next(now)

	return next.Sub(now), nil
}

func (rus *RateUserSubscription) IntervalDuration() (time.Duration, error) {
	if rus.ConditionType != ConditionTypeInterval {
		return 0, fmt.Errorf("invalid condition type: %s", rus.ConditionType)
	}
	d, err := time.ParseDuration(rus.ConditionValue)
	if err != nil {
		return 0, err
	}
	if d < time.Minute {
		return 0, fmt.Errorf("invalid interval: %s", rus.ConditionValue)
	}
	return d, nil
}

type SubscriptionConditionType string

const (
	ConditionTypeDelta    SubscriptionConditionType = "delta"
	ConditionTypeInterval SubscriptionConditionType = "interval"
	ConditionTypeDaily    SubscriptionConditionType = "daily"
	ConditionTypeCron     SubscriptionConditionType = "cron"
)
