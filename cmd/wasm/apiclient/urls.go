package apiclient

import (
	"net/url"
	"strconv"
)

func sourcesURL(limit int) string {
	v := url.Values{}
	v.Set("limit", strconv.Itoa(limit))
	return "/api/sources?" + v.Encode()
}

func ratesURL(name string, limit int) string {
	v := url.Values{}
	v.Set("limit", strconv.Itoa(limit))
	return "/api/sources/" + url.PathEscape(name) + "/rates?" + v.Encode()
}

func subscriptionsURL(name string, page int) string {
	v := url.Values{}
	v.Set("page", strconv.Itoa(page))
	return "/api/sources/" + url.PathEscape(name) + "/subscriptions/list?" + v.Encode()
}

func dailyEventsURL(name string, page int) string {
	v := url.Values{}
	v.Set("page", strconv.Itoa(page))
	return "/api/sources/" + url.PathEscape(name) + "/events/daily?" + v.Encode()
}

func executionErrorsURL(page int) string {
	v := url.Values{}
	v.Set("page", strconv.Itoa(page))
	return "/api/errors/execution?" + v.Encode()
}

func failedNotificationsURL(offset, limit int) string {
	v := url.Values{}
	v.Set("limit", strconv.Itoa(limit))
	v.Set("offset", strconv.Itoa(offset))
	return "/api/notifications/failed?" + v.Encode()
}

func statsURL() string {
	return "/api/stats"
}

func sourceActiveURL(name string) string {
	return "/api/sources/" + url.PathEscape(name) + "/active"
}

func meSubscriptionsURL(page, pageSize int, q string) string {
	v := url.Values{}
	v.Set("page", strconv.Itoa(page))
	v.Set("page_size", strconv.Itoa(pageSize))
	if q != "" {
		v.Set("q", q)
	}
	return "/api/me/subscriptions?" + v.Encode()
}

func meSubscriptionsHeaders(initData string) map[string]string {
	return map[string]string{"X-Telegram-Init-Data": initData}
}

func meProfileURL() string { return "/api/me/profile" }

// meRatesChartURL returns the endpoint for the authenticated sparkline-list chart.
// No query parameters — the 7-day window is fixed server-side.
func meRatesChartURL() string { return "/api/me/rates/chart" }

// publicRatesChartURL returns the paginated public sparkline-list endpoint URL.
// page is 1-based; limit is the page size (1–100).
func publicRatesChartURL(page, limit int) string {
	v := url.Values{}
	v.Set("page", strconv.Itoa(page))
	v.Set("limit", strconv.Itoa(limit))
	return "/api/public/rates/chart?" + v.Encode()
}

// meRatesHistoryURL returns the paginated per-pair history endpoint URL.
// pair should be upper-case canonical (e.g. "USD/KZT"); url.Values.Encode
// percent-encodes the slash. When sourceTitle is non-empty, the source_title
// query parameter is added; otherwise it is omitted entirely.
func meRatesHistoryURL(pair, sourceTitle string, page, limit int) string {
	v := url.Values{}
	v.Set("pair", pair)
	v.Set("page", strconv.Itoa(page))
	v.Set("limit", strconv.Itoa(limit))
	if sourceTitle != "" {
		v.Set("source_title", sourceTitle)
	}
	return "/api/me/rates/history?" + v.Encode()
}
