package domain

import "time"

// RateUserSubscription represents a user's subscription to a monitored rate source.
type RateUserSubscription struct {
	UserType       UserType
	UserID         string
	Source         string
	DeltaThreshold float64 // minimum price change that triggers a user alert
	CreatedAt      time.Time
}

type UserType string

const (
	UserTypeTelegram UserType = "telegrambot"
)
