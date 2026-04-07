// Package handlers contains the HTTP handler implementations for the v1 API.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/gateway/httpV1/dto"
)

func NewHandler(
	srvRate rateService,
) (*Handler, error) {

	h := &Handler{
		rateService: srvRate,
	}

	return h, nil
}

// Handler groups all v1 HTTP handlers and their repository dependencies.
type Handler struct {
	rateService
}

// ListSources returns every configured rate source decorated with its latest execution status.
//
// GET /api/sources
func (h *Handler) ListSources(w http.ResponseWriter, r *http.Request) {
	sources, err := h.rateService.ObtainAllRateSources(r.Context())
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
		if recs, _ := h.rateService.ObtainLastNExecutionHistoryBySourceName(r.Context(), s.Name, 1); len(recs) > 0 {
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
	name, err := extractName(r.URL)
	if err != nil {
		http.Error(w, `{"error":"name must be a number"}`, http.StatusBadRequest)
		return
	}
	limit, err := extractLimit(r.URL)
	if err != nil {
		http.Error(w, `{"error":"limit must be a number"}`, http.StatusBadRequest)
		return
	}

	rates, err := h.rateService.ObtainLastNRateValuesBySourceName(r.Context(), name, limit)
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
	limit, err := extractLimit(r.URL)
	if err != nil {
		http.Error(w, `{"error":"limit must be a number"}`, http.StatusBadRequest)
		return
	}
	name, err := extractName(r.URL)
	if err != nil {
		http.Error(w, `{"error":"name must be a number"}`, http.StatusBadRequest)
		return
	}
	recs, err := h.rateService.ObtainLastNExecutionHistoryBySourceName(r.Context(), name, limit)
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

// ListNotifications returns the last N notification pool records.
// Optional query param ?limit=N (1–100, default 10).
//
// GET /api/notifications
func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	limit, err := extractLimit(r.URL)
	if err != nil {
		http.Error(w, `{"error":"limit must be a number"}`, http.StatusBadRequest)
		return
	}

	records, err := h.rateService.ObtainListOfLastRateUserEvent(r.Context(), limit)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	resp := make([]dto.NotificationResponse, 0, len(records))
	for _, rec := range records {
		resp = append(resp, dto.NotificationResponse{
			ID:        rec.ID,
			UserType:  string(rec.UserType),
			UserID:    rec.UserID,
			Status:    string(rec.Status),
			LastError: rec.LastError,
			CreatedAt: rec.CreatedAt,
			SentAt:    rec.SentAt,
		})
	}
	writeJSON(w, resp)
}

// ListFailedNotifications returns all failed notification pool records.
//
// GET /api/notifications/failed
func (h *Handler) ListFailedNotifications(w http.ResponseWriter, r *http.Request) {
	limit, err := extractLimit(r.URL)
	if err != nil {
		http.Error(w, `{"error":"limit must be a number"}`, http.StatusBadRequest)
		return
	}
	offset, err := extractOffset(r.URL)
	if err != nil {
		http.Error(w, `{"error":"limit must be a number"}`, http.StatusBadRequest)
		return
	}

	records, err := h.rateService.ObtainFailedListOfRateUserEvent(r.Context(), offset, limit)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	resp := make([]dto.NotificationResponse, 0, len(records))
	for _, rec := range records {
		resp = append(resp, dto.NotificationResponse{
			ID:        rec.ID,
			UserType:  string(rec.UserType),
			UserID:    rec.UserID,
			Status:    string(rec.Status),
			LastError: rec.LastError,
			CreatedAt: rec.CreatedAt,
			SentAt:    rec.SentAt,
		})
	}
	writeJSON(w, resp)
}

// writeJSON sets Content-Type and encodes v as JSON.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

type rateService interface {
	ObtainLastNExecutionHistoryBySourceName(ctx context.Context, name string, limit int64) ([]domain.ExecutionHistory, error)
	ObtainLastSuccessNExecutionHistoryBySourceName(ctx context.Context, name string, limit int64) ([]domain.ExecutionHistory, error)
	ObtainAllRateSources(ctx context.Context) ([]domain.RateSource, error)
	ObtainLastNRateValuesBySourceName(ctx context.Context, name string, limit int64) ([]domain.RateValue, error)
	ObtainListOfLastRateUserEvent(ctx context.Context, limit int64) ([]domain.RateUserEvent, error)
	ObtainFailedListOfRateUserEvent(ctx context.Context, offset, limit int64) ([]domain.RateUserEvent, error)
}

func extractLimit(uri *url.URL) (int64, error) {
	var result int64 = 50

	if v := uri.Query().Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return 0, err
		}
		if n > 0 {
			result = n
		}
	}

	result = min(result, 100)
	result = max(result, 10)

	return result, nil
}

func extractOffset(uri *url.URL) (int64, error) {
	var result int64 = 0

	if v := uri.Query().Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			err = errors.Join(err, internal.NewTraceError())
			return 0, err
		}
		if n > 0 {
			result = n
		}
	}

	result = max(result, 0)

	return result, nil
}

func extractName(uri *url.URL) (string, error) {
	var result = ""

	if v := uri.Query().Get("name"); v != "" {
		result = v
	}

	return result, nil
}
