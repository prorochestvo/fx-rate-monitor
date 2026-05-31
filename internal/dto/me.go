package dto

// MeSubscriptionRow is one row in the Mini App subscriptions response.
// Groups all conditions for the same source into a single row.
// UserID is never returned — the endpoint is scoped to the authenticated caller.
type MeSubscriptionRow struct {
	SourceName    string   `json:"source_name"`
	SourceTitle   string   `json:"source_title"`
	BaseCurrency  string   `json:"base_currency"`
	QuoteCurrency string   `json:"quote_currency"`
	Conditions    []string `json:"conditions"`
	LatestPrice   float64  `json:"latest_price,omitempty"`
	LatestAt      string   `json:"latest_at,omitempty"`
}

// MeSubscriptionsResponse is the JSON envelope returned by GET /api/me/subscriptions.
type MeSubscriptionsResponse struct {
	Items    []MeSubscriptionRow `json:"items"`
	Page     int64               `json:"page"`
	PageSize int64               `json:"page_size"`
	Total    int64               `json:"total"`
}

// MeProfileRequest is the JSON body for POST /api/me/profile.
//
// Timezone is an IANA name resolvable by time.LoadLocation
// (e.g. "Asia/Almaty"); the server validates it before persistence and
// returns 400 PublicError on failure.
//
// Locale is a BCP-47 tag (e.g. "ru-RU"). Stored verbatim — the server does
// not validate BCP-47 syntax because the failure mode is cosmetic (a stored
// garbage string just yields no localisation match later). Empty string is
// acceptable: the WASM client always reads Intl, but a non-browser caller
// might omit it.
//
// By project policy this DTO never carries username / display-name / phone /
// email — see the no-PII memory.
type MeProfileRequest struct {
	Timezone string `json:"timezone"`
	Locale   string `json:"locale"`
}
