package sourceaudit

import (
	"bytes"
	"context"
	"math"
	"strconv"

	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/tools/rateextractor"
)

// ProbeStatus describes the outcome of probing a single seeded source.
type ProbeStatus string

const (
	StatusOK                 ProbeStatus = "OK"
	StatusFetchError         ProbeStatus = "FETCH_ERROR"
	StatusHTTPNon2xx         ProbeStatus = "HTTP_NON_2XX"
	StatusRegexNoMatch       ProbeStatus = "REGEX_NO_MATCH"
	StatusJSONPathError      ProbeStatus = "JSON_PATH_ERROR"
	StatusValueParseError    ProbeStatus = "VALUE_PARSE_ERROR"
	StatusUnsupportedMethod  ProbeStatus = "UNSUPPORTED_METHOD"
	StatusUnsupportedHeaders ProbeStatus = "UNSUPPORTED_HEADERS"
)

// ProbeResult is the outcome of auditing one seeded source.
type ProbeResult struct {
	Source  SeededSource
	URL     string
	Status  ProbeStatus
	Value   string
	Detail  string
	Body    []byte
	Content string
}

// Auditor orchestrates fetching and extraction for a list of seeded sources.
type Auditor struct {
	Fetcher Fetcher
}

type fetchEntry struct {
	result *FetchResult
	err    error
}

// Run audits all sources sequentially, deduplicating fetches by URL.
// The output slice is parallel to sources.
func (a *Auditor) Run(ctx context.Context, sources []SeededSource) ([]ProbeResult, error) {
	cache := make(map[string]*fetchEntry)

	urlOrder := make([]string, 0)
	seen := make(map[string]bool)
	for _, s := range sources {
		if !seen[s.URL] {
			seen[s.URL] = true
			urlOrder = append(urlOrder, s.URL)
		}
	}

	for _, u := range urlOrder {
		res, err := a.Fetcher.Fetch(ctx, u)
		cache[u] = &fetchEntry{result: res, err: err}
	}

	results := make([]ProbeResult, len(sources))
	for i, src := range sources {
		results[i] = a.probeSource(src, cache[src.URL])
	}
	return results, nil
}

func (a *Auditor) probeSource(src SeededSource, fetch *fetchEntry) ProbeResult {
	pr := ProbeResult{
		Source: src,
		URL:    src.URL,
	}

	if fetch.err != nil {
		pr.Status = StatusFetchError
		pr.Detail = fetch.err.Error()
		return pr
	}

	pr.Body = fetch.result.Body
	pr.Content = fetch.result.ContentType

	payload := make([]byte, len(fetch.result.Body))
	copy(payload, fetch.result.Body)

	for _, rule := range src.Rules {
		var (
			out []byte
			err error
		)
		switch rule.Method {
		case domain.MethodRegex:
			out, err = rateextractor.ApplyRegex(rule.Pattern, payload)
			if err != nil {
				pr.Status = StatusRegexNoMatch
				pr.Detail = truncate(err.Error(), 80)
				return pr
			}
		case domain.MethodJSONPath:
			out, err = rateextractor.ApplyJSONPath(rule.Pattern, payload)
			if err != nil {
				pr.Status = StatusJSONPathError
				pr.Detail = truncate(err.Error(), 80)
				return pr
			}
		case domain.MethodParseFloat, domain.MethodStoreToRate:
			pr.Status = StatusUnsupportedMethod
			pr.Detail = string(rule.Method)
			return pr
		default:
			pr.Status = StatusUnsupportedMethod
			pr.Detail = string(rule.Method)
			return pr
		}
		payload = bytes.TrimSpace(out)
	}

	payload = bytes.ReplaceAll(payload, []byte(","), []byte("."))
	payload = bytes.ReplaceAll(payload, []byte(" "), []byte(""))

	valueStr := string(payload)

	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		pr.Status = StatusValueParseError
		pr.Detail = truncate(valueStr, 80)
		return pr
	}

	if math.IsNaN(value) || math.IsInf(value, 0) {
		pr.Status = StatusValueParseError
		pr.Detail = truncate(valueStr, 80)
		return pr
	}

	if value <= 0 || value > math.MaxInt32 {
		pr.Status = StatusValueParseError
		pr.Detail = truncate(valueStr, 80)
		return pr
	}

	pr.Status = StatusOK
	pr.Value = valueStr
	return pr
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
