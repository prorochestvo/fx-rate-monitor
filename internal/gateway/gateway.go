// Package gateway is the HTTP front-door for the web server.
// It wires v1 handlers to their route constants and returns a configured *http.ServeMux.
package gateway

import (
	"net/http"

	"github.com/seilbekskindirov/monitor/internal/gateway/httpV1/handlers"
	"github.com/seilbekskindirov/monitor/internal/gateway/httpV1/routes"
)

// Gateway wires HTTP handlers to routes and exposes a ready-to-use ServeMux.
type Gateway struct {
	handler *handlers.Handler
}

// New constructs a Gateway for the given Handler.
func New(h *handlers.Handler) *Gateway {
	return &Gateway{handler: h}
}

// Mux returns a *http.ServeMux pre-wired with all v1 API routes.
// Static file serving is intentionally NOT included here; it is registered
// separately in cmd/web/main.go because it serves a different concern
// (asset delivery) and must be ordered after API routes to avoid shadowing them.
func (g *Gateway) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+routes.Sources, g.handler.ListSources)
	mux.HandleFunc("GET "+routes.SourceRates, g.handler.ListRates)
	mux.HandleFunc("GET "+routes.SourceHistory, g.handler.ListHistory)
	return mux
}
