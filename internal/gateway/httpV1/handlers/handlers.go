// Package handlers contains the HTTP handler implementations for the v1 API.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/gateway/httpV1/dto"
	"github.com/seilbekskindirov/monitor/internal/repository"
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
	name, err := extractName(r)
	if err != nil {
		http.Error(w, `{"error":"missing source name"}`, http.StatusBadRequest)
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
	name, err := extractName(r)
	if err != nil {
		http.Error(w, `{"error":"missing source name"}`, http.StatusBadRequest)
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
	offset, err := extractOffset(r)
	if err != nil {
		http.Error(w, `{"error":"offset must be a number"}`, http.StatusBadRequest)
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

// ListPendingEvents returns all currently pending notification events.
//
// GET /api/events/pending
func (h *Handler) ListPendingEvents(w http.ResponseWriter, r *http.Request) {
	events, err := h.rateService.ObtainPendingRateUserEvents(r.Context())
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	resp := make([]dto.NotificationResponse, 0, len(events))
	for _, e := range events {
		resp = append(resp, dto.NotificationResponse{
			ID:        e.ID,
			UserType:  string(e.UserType),
			Status:    string(e.Status),
			CreatedAt: e.CreatedAt,
		})
	}
	writeJSON(w, resp)
}

// GetRatesChart returns aggregated rate data for a source over a given period.
//
// GET /api/sources/{name}/rates/chart?period=week|month|year
func (h *Handler) GetRatesChart(w http.ResponseWriter, r *http.Request) {
	name, err := extractName(r)
	if err != nil {
		http.Error(w, `{"error":"missing source name"}`, http.StatusBadRequest)
		return
	}
	period := repository.ChartPeriod(r.URL.Query().Get("period"))
	if period == "" {
		period = repository.ChartPeriodWeek
	}
	points, err := h.rateService.ObtainRateValueChartBySourceName(r.Context(), name, period)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	resp := make([]dto.ChartPointResponse, 0, len(points))
	for _, p := range points {
		resp = append(resp, dto.ChartPointResponse{Label: p.Label, Price: p.Price})
	}
	writeJSON(w, resp)
}

// ListSourceFailedEvents returns paginated failed events for a named source.
//
// GET /api/sources/{name}/events/failed?page=N
func (h *Handler) ListSourceFailedEvents(w http.ResponseWriter, r *http.Request) {
	name, err := extractName(r)
	if err != nil {
		http.Error(w, `{"error":"missing source name"}`, http.StatusBadRequest)
		return
	}
	page, _ := strconv.ParseInt(r.URL.Query().Get("page"), 10, 64)
	if page < 1 {
		page = 1
	}
	const pageSize = 50
	events, err := h.rateService.ObtainFailedRateUserEventsBySourceName(r.Context(), name, page, pageSize)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	resp := make([]dto.NotificationResponse, 0, len(events))
	for _, e := range events {
		resp = append(resp, dto.NotificationResponse{
			ID:        e.ID,
			UserType:  string(e.UserType),
			Status:    string(e.Status),
			LastError: e.LastError,
			CreatedAt: e.CreatedAt,
			SentAt:    e.SentAt,
		})
	}
	writeJSON(w, resp)
}

// ListSourceSubscriptions returns grouped subscription + event statistics for a source.
//
// GET /api/sources/{name}/subscriptions
func (h *Handler) ListSourceSubscriptions(w http.ResponseWriter, r *http.Request) {
	name, err := extractName(r)
	if err != nil {
		http.Error(w, `{"error":"missing source name"}`, http.StatusBadRequest)
		return
	}
	summaries, err := h.rateService.ObtainSubscriptionSummaryBySource(r.Context(), name)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	resp := make([]dto.SubscriptionSummaryResponse, 0, len(summaries))
	for _, s := range summaries {
		item := dto.SubscriptionSummaryResponse{
			SourceName:        s.SourceName,
			UserType:          string(s.UserType),
			SubscriptionCount: s.SubscriptionCount,
			SuccessCount:      s.SuccessCount,
			FailedCount:       s.FailedCount,
		}
		if !s.LastSentAt.IsZero() {
			item.LastSentAt = s.LastSentAt.Format(time.RFC3339)
		}
		resp = append(resp, item)
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
	ObtainPendingRateUserEvents(ctx context.Context) ([]domain.RateUserEvent, error)
	ObtainRateValueChartBySourceName(ctx context.Context, name string, period repository.ChartPeriod) ([]repository.ChartPoint, error)
	ObtainFailedRateUserEventsBySourceName(ctx context.Context, sourceName string, page, pageSize int64) ([]domain.RateUserEvent, error)
	ObtainSubscriptionSummaryBySource(ctx context.Context, sourceName string) ([]repository.SubscriptionSummary, error)
}

// extractLimit reads the ?limit= query parameter, clamped to [10, 100], default 50.
func extractLimit(uri *url.URL) (int64, error) {
	var result int64 = 50

	if v := uri.Query().Get("limit"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, errors.Join(err, internal.NewTraceError())
		}
		if n > 0 {
			result = n
		}
	}

	result = min(result, 100)
	result = max(result, 10)

	return result, nil
}

// extractOffset reads the ?offset= query parameter. Returns 0 when absent.
func extractOffset(r *http.Request) (int64, error) {
	var result int64
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, errors.Join(err, internal.NewTraceError())
		}
		if n > 0 {
			result = n
		}
	}
	return max(result, 0), nil
}

// extractName reads the {name} path segment set by Go 1.22's ServeMux.
// Returns an error when the segment is absent so callers can return 400.
func extractName(r *http.Request) (string, error) {
	v := r.PathValue("name")
	if v == "" {
		return "", fmt.Errorf("missing path param: name")
	}
	return v, nil
}
