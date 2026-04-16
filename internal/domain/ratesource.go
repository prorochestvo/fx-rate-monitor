package domain

// RateSource defines a monitored source and its extraction rules.
type RateSource struct {
	Name          string            `json:"name"`
	Title         string            `json:"title"`
	URL           string            `json:"url"`
	Interval      string            `json:"interval"`
	BaseCurrency  string            `json:"base_currency"`
	QuoteCurrency string            `json:"quote_currency"`
	Kind          RateSourceKind    `json:"kind"`
	Active        bool              `json:"active"`
	Options       RateSourceOptions `json:"options"`
	Rules         []RateSourceRule  `json:"rules"`
}

// RateSourceRule defines an extraction rule for a source.
type RateSourceRule struct {
	Method  Method `json:"method"`
	Pattern string `json:"pattern"`
	Options string `json:"options,omitempty"`
}

type RateSourceOptions struct {
	Reserve string `json:"reserve,omitempty"`
}

type Method string

const (
	MethodParseFloat  Method = "parse_float"
	MethodRegex       Method = "regex"
	MethodJSONPath    Method = "json"
	MethodStoreToRate Method = "store_as_rate"
)

type RateSourceKind string

const (
	RateSourceKindBID = "BID"
	RateSourceKindASK = "ASK"
)
