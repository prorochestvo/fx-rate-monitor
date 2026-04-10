// Package dto defines the HTTP response Data Transfer Objects for the v1 API.
package dto

import "time"

// SourceResponse is the JSON representation of a configured rate source,
// decorated with its most recent execution status.
type SourceResponse struct {
	Name          string `json:"name"`
	BaseCurrency  string `json:"base_currency"`
	QuoteCurrency string `json:"quote_currency"`
	Interval      string `json:"interval"`
	LastSuccess   bool   `json:"last_success"`
	LastError     string `json:"last_error,omitempty"`
	LastRunAt     string `json:"last_run_at,omitempty"`
}

// RateResponse is the JSON representation of a stored rate value.
type RateResponse struct {
	ID            string  `json:"id"`
	BaseCurrency  string  `json:"base_currency"`
	QuoteCurrency string  `json:"quote_currency"`
	Price         float64 `json:"price"`
	Timestamp     string  `json:"timestamp"`
}

// HistoryResponse is the JSON representation of a single execution history record.
type HistoryResponse struct {
	ID        string `json:"id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

// NotificationResponse is the JSON representation of a notification pool record.
// The message body is intentionally omitted to avoid leaking rate content through the API.
// UserID is omitted when empty to prevent leaking subscriber identifiers via endpoints that
// do not require it (e.g. failed-events per source).
type NotificationResponse struct {
	ID        string    `json:"id"`
	UserType  string    `json:"user_type"`
	UserID    string    `json:"user_id,omitempty"`
	Status    string    `json:"status"`
	LastError string    `json:"last_error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	SentAt    time.Time `json:"sent_at"`
}

// ChartPointResponse is the JSON shape of one aggregated rate data point.
type ChartPointResponse struct {
	Label string  `json:"label"`
	Price float64 `json:"price"`
}

// SubscriptionSummaryResponse is the JSON shape of one source subscription summary row.
// UserID is never included so subscriber identifiers are not leaked via the API.
type SubscriptionSummaryResponse struct {
	SourceName        string `json:"source_name"`
	UserType          string `json:"user_type"`
	SubscriptionCount int64  `json:"subscription_count"`
	LastSentAt        string `json:"last_sent_at,omitempty"`
	SuccessCount      int64  `json:"success_count"`
	FailedCount       int64  `json:"failed_count"`
}
