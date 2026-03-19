// Package handlers contains the HTTP handler implementations for the v1 API.
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/gateway/httpV1/dto"
)

// Handler groups all v1 HTTP handlers and their repository dependencies.
type Handler struct {
	SourceRepo       rateSourceRepository
	RateValueRepo    rateValueRepository
	ExecHistoryRepo  executionHistoryRepository
	UserSubscription userSubscriptionRepository
}

// ListSources returns every configured rate source decorated with its latest execution status.
//
// GET /api/sources
func (h *Handler) ListSources(w http.ResponseWriter, r *http.Request) {
	sources, err := h.SourceRepo.ObtainAllRateSources(r.Context())
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	resp := make([]dto.SourceResponse, 0, len(sources))
	for _, s := range sources {
		item := dto.SourceResponse{
			Name:          s.Name,
			BaseCurrency:  s.BaseCurrency,
			QuoteCurrency: s.QuoteCurrency,
			Interval:      s.Interval,
		}
		if recs, _ := h.ExecHistoryRepo.ObtainLastNExecutionHistoryBySourceName(r.Context(), s.Name, 1, false); len(recs) > 0 {
			item.LastSuccess = recs[0].Success
			item.LastError = recs[0].Error
			item.LastRunAt = recs[0].Timestamp.Format(time.RFC3339)
		}
		resp = append(resp, item)
	}
	writeJSON(w, resp)
}

// ListRates returns the most recent rate values for a named source.
// Optional query param ?limit=N (1–1000, default 100).
//
// GET /api/sources/{name}/rates
func (h *Handler) ListRates(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	rates, err := h.RateValueRepo.ObtainLastNRateValuesBySourceName(r.Context(), name, limit)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	resp := make([]dto.RateResponse, 0, len(rates))
	for _, rv := range rates {
		resp = append(resp, dto.RateResponse{
			ID:            rv.ID,
			Price:         rv.Price,
			BaseCurrency:  rv.BaseCurrency,
			QuoteCurrency: rv.QuoteCurrency,
			Timestamp:     rv.Timestamp.Format(time.RFC3339),
		})
	}
	writeJSON(w, resp)
}

// ListHistory returns the 50 most recent execution history records for a named source.
//
// GET /api/sources/{name}/history
func (h *Handler) ListHistory(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	recs, err := h.ExecHistoryRepo.ObtainLastNExecutionHistoryBySourceName(r.Context(), name, 50, false)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	resp := make([]dto.HistoryResponse, 0, len(recs))
	for _, rec := range recs {
		resp = append(resp, dto.HistoryResponse{
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
	ObtainAllRateSources(context.Context) ([]domain.RateSource, error)
}

type rateValueRepository interface {
	ObtainLastNRateValuesBySourceName(context.Context, string, int) ([]domain.RateValue, error)
}

type executionHistoryRepository interface {
	ObtainLastNExecutionHistoryBySourceName(context.Context, string, int, bool) ([]domain.ExecutionHistory, error)
}

type userSubscriptionRepository interface {
	ObtainRateUserSubscriptionsBySource(context.Context, string) ([]domain.RateUserSubscription, error)
}
