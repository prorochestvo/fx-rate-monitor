// Package routes centralises all HTTP route path constants for the v1 API.
// Using constants prevents typos and makes the full API surface auditable at a glance.
package routes

const (
	// Sources lists all configured rate sources.
	Sources = "/api/sources"

	// SourceRatesChart returns aggregated chart data for a named source.
	// Must be registered BEFORE SourceRates so the more-specific pattern wins.
	SourceRatesChart = "/api/sources/{name}/rates/chart"

	// SourceRates returns recent rate values for a named source.
	// The {name} segment maps to r.PathValue("name") in Go 1.22+.
	SourceRates = "/api/sources/{name}/rates"

	// SourceHistory returns execution history for a named source.
	SourceHistory = "/api/sources/{name}/history"

	// SourceEventsFailed returns paginated failed events for a named source.
	SourceEventsFailed = "/api/sources/{name}/events/failed"

	// SourceSubscriptions returns grouped subscription statistics for a named source.
	SourceSubscriptions = "/api/sources/{name}/subscriptions"

	// EventsPending returns all currently pending notification events.
	EventsPending = "/api/events/pending"

	// Notifications returns the last N notification pool records.
	// Registered before NotificationsFailed to avoid prefix shadowing.
	Notifications = "/api/notifications"

	// NotificationsFailed returns all failed notification pool records.
	NotificationsFailed = "/api/notifications/failed"
)
