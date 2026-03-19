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
	"github.com/seilbekskindirov/monitor/internal/gateway/httpV1/routes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+routes.Sources, h.ListSources)
	mux.HandleFunc("GET "+routes.SourceRates, h.ListRates)
	mux.HandleFunc("GET "+routes.SourceHistory, h.ListHistory)
	return mux
}

// --- ListSources ---

func TestListSources_EmptySliceNotNull(t *testing.T) {
	t.Parallel()

	h := &Handler{SourceRepo: &stubSourceRepo{}, RateValueRepo: &stubRateRepo{}, ExecHistoryRepo: &stubHistoryRepo{}}
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var result []dto.SourceResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestListSources_PopulatesLastRunFields(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC)
	src := domain.RateSource{Name: "halyk_bank", BaseCurrency: "USD", QuoteCurrency: "KZT", Interval: "10m"}
	hist := domain.ExecutionHistory{ID: "H1", SourceName: "halyk_bank", Success: true, Timestamp: ts}

	h := &Handler{
		SourceRepo:      &stubSourceRepo{sources: []domain.RateSource{src}},
		RateValueRepo:   &stubRateRepo{},
		ExecHistoryRepo: &stubHistoryRepo{records: map[string][]domain.ExecutionHistory{"halyk_bank": {hist}}},
	}
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var result []dto.SourceResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Len(t, result, 1)
	assert.True(t, result[0].LastSuccess)
	assert.Equal(t, ts.Format(time.RFC3339), result[0].LastRunAt)
}

func TestListSources_RepoError_Returns500(t *testing.T) {
	t.Parallel()

	h := &Handler{
		SourceRepo:      &stubSourceRepo{err: errors.New("db down")},
		RateValueRepo:   &stubRateRepo{},
		ExecHistoryRepo: &stubHistoryRepo{},
	}
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/sources", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

// --- ListRates ---

func TestListRates_DefaultLimit(t *testing.T) {
	t.Parallel()

	rates := make([]domain.RateValue, 5)
	for i := range rates {
		rates[i] = domain.RateValue{ID: "RV" + string(rune('0'+i)), SourceName: "halyk_bank", Price: float64(i)}
	}

	h := &Handler{
		SourceRepo:      &stubSourceRepo{},
		RateValueRepo:   &stubRateRepo{rates: map[string][]domain.RateValue{"halyk_bank": rates}},
		ExecHistoryRepo: &stubHistoryRepo{},
	}
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/sources/halyk_bank/rates", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var result []dto.RateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Len(t, result, 5)
}

func TestListRates_NonexistentSource_EmptyArray(t *testing.T) {
	t.Parallel()

	h := &Handler{SourceRepo: &stubSourceRepo{}, RateValueRepo: &stubRateRepo{}, ExecHistoryRepo: &stubHistoryRepo{}}
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/sources/nonexistent/rates", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var result []dto.RateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.NotNil(t, result)
	require.Empty(t, result)
}

// --- ListHistory ---

func TestListHistory_EmptyArray(t *testing.T) {
	t.Parallel()

	h := &Handler{SourceRepo: &stubSourceRepo{}, RateValueRepo: &stubRateRepo{}, ExecHistoryRepo: &stubHistoryRepo{}}
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/sources/halyk_bank/history", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var result []dto.HistoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.NotNil(t, result)
	require.Empty(t, result)
}

func TestListHistory_ReturnsRecords(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC)
	records := []domain.ExecutionHistory{
		{ID: "H1", SourceName: "halyk_bank", Success: true, Timestamp: ts},
		{ID: "H2", SourceName: "halyk_bank", Success: false, Error: "timeout", Timestamp: ts.Add(-time.Minute)},
	}

	h := &Handler{
		SourceRepo:      &stubSourceRepo{},
		RateValueRepo:   &stubRateRepo{},
		ExecHistoryRepo: &stubHistoryRepo{records: map[string][]domain.ExecutionHistory{"halyk_bank": records}},
	}
	mux := newTestMux(h)

	req := httptest.NewRequest(http.MethodGet, "/api/sources/halyk_bank/history", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var result []dto.HistoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Len(t, result, 2)
	assert.True(t, result[0].Success)
	assert.False(t, result[1].Success)
	assert.Equal(t, "timeout", result[1].Error)
}

// --- stub implementations ---

type stubSourceRepo struct {
	sources []domain.RateSource
	err     error
}

func (s *stubSourceRepo) ObtainAllRateSources(_ context.Context) ([]domain.RateSource, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.sources == nil {
		return []domain.RateSource{}, nil
	}
	return s.sources, nil
}

type stubRateRepo struct {
	rates map[string][]domain.RateValue
	err   error
}

func (s *stubRateRepo) ObtainLastNRateValuesBySourceName(_ context.Context, sourceName string, _ int) ([]domain.RateValue, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.rates == nil {
		return []domain.RateValue{}, nil
	}
	if v, ok := s.rates[sourceName]; ok {
		return v, nil
	}
	return []domain.RateValue{}, nil
}

type stubHistoryRepo struct {
	records map[string][]domain.ExecutionHistory
	err     error
}

func (s *stubHistoryRepo) ObtainLastNExecutionHistoryBySourceName(_ context.Context, sourceName string, _ int, _ bool) ([]domain.ExecutionHistory, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.records == nil {
		return []domain.ExecutionHistory{}, nil
	}
	if v, ok := s.records[sourceName]; ok {
		return v, nil
	}
	return []domain.ExecutionHistory{}, nil
}
