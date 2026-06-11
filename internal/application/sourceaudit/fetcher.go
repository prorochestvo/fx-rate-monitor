package sourceaudit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var _ Fetcher = (*httpFetcher)(nil)

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

// NewHTTPFetcher constructs an httpFetcher with the given per-request timeout.
// proxyURL is an optional HTTP proxy URL string (e.g. "http://127.0.0.1:7788");
// pass "" to use no proxy.
//
// When proxyURL is empty an explicit &http.Transport{} with no Proxy field is
// used. A nil Transport would fall back to http.DefaultTransport whose Proxy
// reads HTTPS_PROXY/HTTP_PROXY from the process environment, which would
// silently route traffic even when no proxy was configured by the caller.
func NewHTTPFetcher(timeout time.Duration, proxyURL string) (Fetcher, error) {
	var client *http.Client
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, errors.New("parse proxy URL: invalid format (value redacted from log; check the configured proxy URL)")
		}
		client = &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{Proxy: http.ProxyURL(parsed)},
		}
	} else {
		client = &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{}, // explicit empty transport — no Proxy field, no env auto-pickup
		}
	}
	return &httpFetcher{
		client:  client,
		timeout: timeout,
	}, nil
}

// httpFetcher is the production Fetcher implementation.
// It is not tested directly against the network; coverage comes from integration
// via cmd/doctor audit.
type httpFetcher struct {
	client  *http.Client
	timeout time.Duration
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
