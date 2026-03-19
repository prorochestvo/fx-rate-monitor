// Package dto defines the HTTP response Data Transfer Objects for the v1 API.
package dto

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
