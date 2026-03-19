// Package routes centralises all HTTP route path constants for the v1 API.
// Using constants prevents typos and makes the full API surface auditable at a glance.
package routes

const (
	// Sources lists all configured rate sources.
	Sources = "/api/sources"

	// SourceRates returns recent rate values for a named source.
	// The {name} segment maps to r.PathValue("name") in Go 1.22+.
	SourceRates = "/api/sources/{name}/rates"

	// SourceHistory returns execution history for a named source.
	SourceHistory = "/api/sources/{name}/history"
)
