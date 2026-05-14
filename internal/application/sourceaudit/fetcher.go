package sourceaudit

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultUserAgent is the User-Agent header sent by the audit tool, matching the
// production extractor value.
const DefaultUserAgent = "FXRateMonitor/1.0 (+https://github.com/seilbekskindirov/monitor)"

// FetchResult holds the response from a single HTTP GET.
type FetchResult struct {
	Body        []byte
	ContentType string
	StatusCode  int
}

// Fetcher performs HTTP GETs and returns the body with metadata.
type Fetcher interface {
	Fetch(ctx context.Context, url string) (*FetchResult, error)
}

// httpFetcher is the production Fetcher implementation.
// It is not tested directly against the network; coverage comes from integration
// via cmd/sourceaudit.
type httpFetcher struct {
	client  *http.Client
	timeout time.Duration
}

var _ Fetcher = (*httpFetcher)(nil)

// NewHTTPFetcher constructs an httpFetcher with the given per-request timeout.
func NewHTTPFetcher(timeout time.Duration) *httpFetcher {
	return &httpFetcher{
		client:  &http.Client{Timeout: timeout},
		timeout: timeout,
	}
}

func (f *httpFetcher) Fetch(ctx context.Context, url string) (*FetchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", DefaultUserAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer func(c io.Closer) { _ = c.Close() }(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: unexpected status %d (%s)", url, resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	return &FetchResult{
		Body:        body,
		ContentType: resp.Header.Get("Content-Type"),
		StatusCode:  resp.StatusCode,
	}, nil
}
