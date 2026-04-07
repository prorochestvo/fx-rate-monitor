package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/gateway/httpV1/dto"
	"github.com/stretchr/testify/require"
)

func TestListSources(t *testing.T) {
	t.Parallel()

	t.Run("200 with sources", func(t *testing.T) {
		t.Parallel()

		svc := &mockRateService{
			sources: []domain.RateSource{
				{Name: "src1", BaseCurrency: "USD", QuoteCurrency: "KZT", Interval: "1h"},
				{Name: "src2", BaseCurrency: "EUR", QuoteCurrency: "KZT", Interval: "2h"},
			},
			historyItems: []domain.ExecutionHistory{{
				ID:        "h1",
				Success:   true,
				Timestamp: time.Now().UTC(),
			}},
		}

		h, err := NewHandler(svc)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ListSources(rr, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var body []dto.SourceResponse
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
		require.Len(t, body, 2)
		require.Equal(t, "src1", body[0].Name)
	})

	t.Run("200 empty array when no sources", func(t *testing.T) {
		t.Parallel()

		svc := &mockRateService{sources: nil}
		h, err := NewHandler(svc)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ListSources(rr, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Equal(t, http.StatusOK, rr.Code)

		var body []dto.SourceResponse
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
		require.Empty(t, body)
	})

	t.Run("500 on ObtainAllRateSources error", func(t *testing.T) {
		t.Parallel()

		svc := &mockRateService{err: errors.New("db error")}
		h, err := NewHandler(svc)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ListSources(rr, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}

func TestListRates(t *testing.T) {
	t.Parallel()

	t.Run("200 with rates", func(t *testing.T) {
		t.Parallel()

		svc := &mockRateService{
			rates: []domain.RateValue{
				{ID: "r1", Price: 470.0, BaseCurrency: "USD", QuoteCurrency: "KZT", Timestamp: time.Now().UTC()},
				{ID: "r2", Price: 471.0, BaseCurrency: "USD", QuoteCurrency: "KZT", Timestamp: time.Now().UTC()},
			},
		}
		h, err := NewHandler(svc)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ListRates(rr, httptest.NewRequest(http.MethodGet, "/?name=src1", nil))

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var body []dto.RateResponse
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
		require.Len(t, body, 2)
	})

	t.Run("200 empty", func(t *testing.T) {
		t.Parallel()

		svc := &mockRateService{rates: nil}
		h, err := NewHandler(svc)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ListRates(rr, httptest.NewRequest(http.MethodGet, "/?name=src1", nil))

		require.Equal(t, http.StatusOK, rr.Code)

		var body []dto.RateResponse
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
		require.Empty(t, body)
	})

	t.Run("500 on error", func(t *testing.T) {
		t.Parallel()

		svc := &mockRateService{err: errors.New("db error")}
		h, err := NewHandler(svc)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ListRates(rr, httptest.NewRequest(http.MethodGet, "/?name=src1", nil))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}

func TestListHistory(t *testing.T) {
	t.Parallel()

	t.Run("200 with records", func(t *testing.T) {
		t.Parallel()

		svc := &mockRateService{
			historyItems: []domain.ExecutionHistory{
				{ID: "h1", Success: true, Timestamp: time.Now().UTC()},
				{ID: "h2", Success: false, Error: "oops", Timestamp: time.Now().UTC()},
				{ID: "h3", Success: true, Timestamp: time.Now().UTC()},
			},
		}
		h, err := NewHandler(svc)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ListHistory(rr, httptest.NewRequest(http.MethodGet, "/?name=src1", nil))

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var body []dto.HistoryResponse
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
		require.Len(t, body, 3)
	})

	t.Run("500 on error", func(t *testing.T) {
		t.Parallel()

		svc := &mockRateService{err: errors.New("db error")}
		h, err := NewHandler(svc)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ListHistory(rr, httptest.NewRequest(http.MethodGet, "/?name=src1", nil))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}

func TestListNotifications(t *testing.T) {
	t.Parallel()

	t.Run("200 with notifications", func(t *testing.T) {
		t.Parallel()

		now := time.Now().UTC()
		svc := &mockRateService{
			events: []domain.RateUserEvent{
				{ID: "e1", UserType: domain.UserTypeTelegram, UserID: "111", Status: domain.RateUserEventStatusSent, CreatedAt: now},
				{ID: "e2", UserType: domain.UserTypeTelegram, UserID: "222", Status: domain.RateUserEventStatusFailed, CreatedAt: now},
			},
		}
		h, err := NewHandler(svc)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ListNotifications(rr, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var body []dto.NotificationResponse
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
		require.Len(t, body, 2)
		require.NotEmpty(t, body[0].ID)
		require.NotEmpty(t, body[0].UserType)
		require.NotEmpty(t, body[0].Status)
	})

	t.Run("500 on error", func(t *testing.T) {
		t.Parallel()

		svc := &mockRateService{err: errors.New("db error")}
		h, err := NewHandler(svc)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ListNotifications(rr, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}

func TestListFailedNotifications(t *testing.T) {
	t.Parallel()

	t.Run("200 with results", func(t *testing.T) {
		t.Parallel()

		now := time.Now().UTC()
		svc := &mockRateService{
			events: []domain.RateUserEvent{
				{ID: "e1", UserType: domain.UserTypeTelegram, UserID: "111", Status: domain.RateUserEventStatusFailed, CreatedAt: now},
			},
		}
		h, err := NewHandler(svc)
		require.NoError(t, err)

		// extractOffset reads the "limit" param (known production bug) — test actual behaviour.
		rr := httptest.NewRecorder()
		h.ListFailedNotifications(rr, httptest.NewRequest(http.MethodGet, "/?limit=20", nil))

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var body []dto.NotificationResponse
		require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
		require.Len(t, body, 1)
	})

	t.Run("500 on error", func(t *testing.T) {
		t.Parallel()

		svc := &mockRateService{err: errors.New("db error")}
		h, err := NewHandler(svc)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		h.ListFailedNotifications(rr, httptest.NewRequest(http.MethodGet, "/", nil))

		require.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}

type mockRateService struct {
	sources      []domain.RateSource
	rates        []domain.RateValue
	historyItems []domain.ExecutionHistory
	events       []domain.RateUserEvent
	err          error
}

func (m *mockRateService) ObtainAllRateSources(_ context.Context) ([]domain.RateSource, error) {
	return m.sources, m.err
}

func (m *mockRateService) ObtainLastNRateValuesBySourceName(_ context.Context, _ string, _ int64) ([]domain.RateValue, error) {
	return m.rates, m.err
}

func (m *mockRateService) ObtainLastNExecutionHistoryBySourceName(_ context.Context, _ string, _ int64) ([]domain.ExecutionHistory, error) {
	return m.historyItems, m.err
}

func (m *mockRateService) ObtainLastSuccessNExecutionHistoryBySourceName(_ context.Context, _ string, _ int64) ([]domain.ExecutionHistory, error) {
	return m.historyItems, m.err
}

func (m *mockRateService) ObtainListOfLastRateUserEvent(_ context.Context, _ int64) ([]domain.RateUserEvent, error) {
	return m.events, m.err
}

func (m *mockRateService) ObtainFailedListOfRateUserEvent(_ context.Context, _, _ int64) ([]domain.RateUserEvent, error) {
	return m.events, m.err
}
