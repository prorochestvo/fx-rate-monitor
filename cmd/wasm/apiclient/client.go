// Package apiclient provides a typed Go client for the /api/... HTTP routes.
// It is used by the WASM frontend; transport is abstracted through the Fetcher
// interface so tests can inject an in-process fake without real HTTP calls.
package apiclient

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/seilbekskindirov/monitor/internal/dto"
)

// Client is a typed HTTP client for the /api/... routes consumed by the WASM frontend.
// Construct one per WASM lifetime via New and inject it into the application layer.
// The client is free of DOM concerns; transport is delegated to the Fetcher.
type Client struct {
	fetcher Fetcher
}

// New constructs a Client backed by the given Fetcher.
func New(f Fetcher) *Client { return &Client{fetcher: f} }

// ListSources fetches all configured rate sources with their latest execution status.
func (c *Client) ListSources(ctx context.Context, limit int) ([]dto.SourceResponse, error) {
	raw, err := c.fetcher.FetchJSON(ctx, "GET", sourcesURL(limit), nil, nil)
	if err != nil {
		return nil, err
	}
	var out []dto.SourceResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode sources: %w", err)
	}
	return out, nil
}

// ListRates fetches the most recent rate values for the named source.
func (c *Client) ListRates(ctx context.Context, name string, limit int) ([]dto.RateResponse, error) {
	raw, err := c.fetcher.FetchJSON(ctx, "GET", ratesURL(name, limit), nil, nil)
	if err != nil {
		return nil, err
	}
	var out []dto.RateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode rates: %w", err)
	}
	return out, nil
}

// ListSubscriptions fetches a page of subscription details for the named source.
func (c *Client) ListSubscriptions(ctx context.Context, name string, page int) ([]dto.SubscriptionDetailResponse, error) {
	raw, err := c.fetcher.FetchJSON(ctx, "GET", subscriptionsURL(name, page), nil, nil)
	if err != nil {
		return nil, err
	}
	var out []dto.SubscriptionDetailResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode subscriptions: %w", err)
	}
	return out, nil
}

// ListDailyEvents fetches a page of daily event summaries for the named source.
func (c *Client) ListDailyEvents(ctx context.Context, name string, page int) ([]dto.DailyEventResponse, error) {
	raw, err := c.fetcher.FetchJSON(ctx, "GET", dailyEventsURL(name, page), nil, nil)
	if err != nil {
		return nil, err
	}
	var out []dto.DailyEventResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode daily events: %w", err)
	}
	return out, nil
}

// ListExecutionErrors fetches a page of failed execution history records across all sources.
func (c *Client) ListExecutionErrors(ctx context.Context, page int) ([]dto.ExecutionErrorResponse, error) {
	raw, err := c.fetcher.FetchJSON(ctx, "GET", executionErrorsURL(page), nil, nil)
	if err != nil {
		return nil, err
	}
	var out []dto.ExecutionErrorResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode execution errors: %w", err)
	}
	return out, nil
}

// ListFailedNotifications fetches a window of failed notification pool records.
// The server uses ?offset=&limit= (not ?page=), mirroring index.html:333.
func (c *Client) ListFailedNotifications(ctx context.Context, offset, limit int) ([]dto.NotificationResponse, error) {
	raw, err := c.fetcher.FetchJSON(ctx, "GET", failedNotificationsURL(offset, limit), nil, nil)
	if err != nil {
		return nil, err
	}
	var out []dto.NotificationResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode failed notifications: %w", err)
	}
	return out, nil
}

// Stats fetches global statistics: source counts and total error count.
func (c *Client) Stats(ctx context.Context) (dto.StatsResponse, error) {
	raw, err := c.fetcher.FetchJSON(ctx, "GET", statsURL(), nil, nil)
	if err != nil {
		return dto.StatsResponse{}, err
	}
	var out dto.StatsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return dto.StatsResponse{}, fmt.Errorf("decode stats: %w", err)
	}
	return out, nil
}

// SetSourceActive enables or disables the named source.
// The server returns 204 No Content on success; no response body is decoded.
func (c *Client) SetSourceActive(ctx context.Context, name string, active bool) error {
	return c.fetcher.FetchNoContent(ctx, "PATCH", sourceActiveURL(name), dto.SourceActiveRequest{Active: active}, nil)
}

// MeSubscriptions fetches the caller's own subscriptions enriched with the latest rate
// values. initData is the Telegram WebApp initData string read by the caller from
// window.Telegram.WebApp.initData; this method sets the X-Telegram-Init-Data header
// from that parameter — it does not read js.Global() itself.
func (c *Client) MeSubscriptions(ctx context.Context, initData string, page, pageSize int, q string) (dto.MeSubscriptionsResponse, error) {
	raw, err := c.fetcher.FetchJSON(ctx, "GET", meSubscriptionsURL(page, pageSize, q), nil, meSubscriptionsHeaders(initData))
	if err != nil {
		return dto.MeSubscriptionsResponse{}, err
	}
	var out dto.MeSubscriptionsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return dto.MeSubscriptionsResponse{}, fmt.Errorf("decode me subscriptions: %w", err)
	}
	return out, nil
}

// UpdateMeProfile sends the caller's IANA timezone and BCP-47 locale to the
// server. Fire-and-forget from the Mini App mount path: the server validates
// and persists, the client ignores everything but a non-nil error. initData
// carries the WebApp HMAC header same as MeSubscriptions.
//
// Locale may be empty when the browser does not expose Intl; the server
// stores it verbatim either way. By project policy this call never carries
// username / display name — see the no-PII memory.
func (c *Client) UpdateMeProfile(ctx context.Context, initData, timezone, locale string) error {
	return c.fetcher.FetchNoContent(ctx, "POST", meProfileURL(),
		dto.MeProfileRequest{Timezone: timezone, Locale: locale},
		meSubscriptionsHeaders(initData))
}

// MeRatesChart fetches the sparkline-list chart data for the calling user's
// subscribed currency pairs over the last 7 days. initData is the Telegram
// WebApp initData string; it is forwarded via the X-Telegram-Init-Data header
// (never as a query parameter — see the privacy notes in routes.go).
func (c *Client) MeRatesChart(ctx context.Context, initData string) (dto.MeChartResponse, error) {
	raw, err := c.fetcher.FetchJSON(ctx, "GET", meRatesChartURL(), nil, meSubscriptionsHeaders(initData))
	if err != nil {
		return dto.MeChartResponse{}, err
	}
	var out dto.MeChartResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return dto.MeChartResponse{}, fmt.Errorf("decode me rates chart: %w", err)
	}
	return out, nil
}

// PublicRatesChart fetches the paginated system-wide sparkline-list chart.
// No authentication header is sent. page is 1-based; limit is the page size
// (the server clamps to [1, 100]).
func (c *Client) PublicRatesChart(ctx context.Context, page, limit int) (dto.PublicChartResponse, error) {
	raw, err := c.fetcher.FetchJSON(ctx, "GET", publicRatesChartURL(page, limit), nil, nil)
	if err != nil {
		return dto.PublicChartResponse{}, err
	}
	var out dto.PublicChartResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return dto.PublicChartResponse{}, fmt.Errorf("decode public rates chart: %w", err)
	}
	return out, nil
}

// MeRatesHistory fetches one page of per-pair rate-collection events for the
// calling user. initData is the Telegram WebApp initData string; pair is a
// canonical "BASE/QUOTE" label; page is 1-based; limit is bounded server-side
// at 100. Returns the parsed JSON envelope.
func (c *Client) MeRatesHistory(ctx context.Context, initData, pair string, page, limit int) (dto.MeHistoryResponse, error) {
	raw, err := c.fetcher.FetchJSON(ctx, "GET", meRatesHistoryURL(pair, page, limit), nil, meSubscriptionsHeaders(initData))
	if err != nil {
		return dto.MeHistoryResponse{}, err
	}
	var out dto.MeHistoryResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return dto.MeHistoryResponse{}, fmt.Errorf("decode me rates history: %w", err)
	}
	return out, nil
}
