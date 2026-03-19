package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
)

// --- response types ---

type sourceResponse struct {
	Name          string `json:"name"`
	BaseCurrency  string `json:"base_currency"`
	QuoteCurrency string `json:"quote_currency"`
	Interval      string `json:"interval"`
	LastSuccess   bool   `json:"last_success"`
	LastError     string `json:"last_error,omitempty"`
	LastRunAt     string `json:"last_run_at,omitempty"`
}

type rateResponse struct {
	ID            string  `json:"id"`
	Price         float64 `json:"price"`
	BaseCurrency  string  `json:"base_currency"`
	QuoteCurrency string  `json:"quote_currency"`
	Timestamp     string  `json:"timestamp"`
}

type historyResponse struct {
	ID        string `json:"id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

// --- handler ---

// Handler exposes read-only JSON endpoints for sources, rates, and execution history.
type Handler struct {
	sources rateSourceRepository
	rates   rateValueRepository
	history executionHistoryRepository
}

// NewHandler constructs a Handler with the three required read-only repositories.
func NewHandler(
	sources rateSourceRepository,
	rates rateValueRepository,
	history executionHistoryRepository,
) *Handler {
	return &Handler{sources: sources, rates: rates, history: history}
}

// Register wires all API routes into mux using Go 1.22+ method+path syntax.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sources", h.listSources)
	mux.HandleFunc("GET /api/sources/{name}/rates", h.listRates)
	mux.HandleFunc("GET /api/sources/{name}/history", h.listHistory)
}

// listSources returns every configured rate source, decorated with the most recent
// execution result (success flag, error string, run timestamp).
func (h *Handler) listSources(w http.ResponseWriter, r *http.Request) {
	sources, err := h.sources.ObtainAllRateSources(r.Context())
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	resp := make([]sourceResponse, 0, len(sources))
	for _, s := range sources {
		item := sourceResponse{
			Name:          s.Name,
			BaseCurrency:  s.BaseCurrency,
			QuoteCurrency: s.QuoteCurrency,
			Interval:      s.Interval,
		}
		if recs, _ := h.history.ObtainLastNExecutionHistoryBySourceName(r.Context(), s.Name, 1, false); len(recs) > 0 {
			item.LastSuccess = recs[0].Success
			item.LastError = recs[0].Error
			item.LastRunAt = recs[0].Timestamp.Format(time.RFC3339)
		}
		resp = append(resp, item)
	}
	writeJSON(w, resp)
}

// listRates returns the most recent rate values for a named source.
// Optional query param ?limit=N (1–1000, default 100).
func (h *Handler) listRates(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	rates, err := h.rates.ObtainLastNRateValuesBySourceName(r.Context(), name, limit)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	resp := make([]rateResponse, 0, len(rates))
	for _, rv := range rates {
		resp = append(resp, rateResponse{
			ID:            rv.ID,
			Price:         rv.Price,
			BaseCurrency:  rv.BaseCurrency,
			QuoteCurrency: rv.QuoteCurrency,
			Timestamp:     rv.Timestamp.Format(time.RFC3339),
		})
	}
	writeJSON(w, resp)
}

// listHistory returns the 50 most recent execution history records for a named source.
func (h *Handler) listHistory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	recs, err := h.history.ObtainLastNExecutionHistoryBySourceName(r.Context(), name, 50, false)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	resp := make([]historyResponse, 0, len(recs))
	for _, rec := range recs {
		resp = append(resp, historyResponse{
			ID:        rec.ID,
			Success:   rec.Success,
			Error:     rec.Error,
			Timestamp: rec.Timestamp.Format(time.RFC3339),
		})
	}
	writeJSON(w, resp)
}

// writeJSON sets Content-Type and encodes v as JSON.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// --- private repository interfaces ---

type rateSourceRepository interface {
	ObtainAllRateSources(ctx context.Context) ([]*domain.RateSource, error)
}

type rateValueRepository interface {
	ObtainLastNRateValuesBySourceName(ctx context.Context, sourceName string, limit int) ([]*domain.RateValue, error)
}

type executionHistoryRepository interface {
	ObtainLastNExecutionHistoryBySourceName(ctx context.Context, sourceName string, limit int, successOnly bool) ([]*domain.ExecutionHistory, error)
}
