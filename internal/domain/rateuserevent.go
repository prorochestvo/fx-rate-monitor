package domain

import "time"

// RateUserEvent represents a single outbound notification stored in the pool.
// It is persisted before delivery and retains LastError and Status for audit and retry.
type RateUserEvent struct {
	ID         string
	SourceName string // name of the rate source that triggered the event
	UserType   UserType
	UserID     string
	Message    string
	Status     RateUserEventStatus
	LastError  string // empty when no error
	SentAt     time.Time
	CreatedAt  time.Time
}

// RateUserEventStatus represents the delivery state of a notification in the pool.
type RateUserEventStatus string

const (
	RateUserEventStatusPending  RateUserEventStatus = "pending"
	RateUserEventStatusSent     RateUserEventStatus = "sent"
	RateUserEventStatusFailed   RateUserEventStatus = "failed"
	RateUserEventStatusCanceled RateUserEventStatus = "canceled"
)
