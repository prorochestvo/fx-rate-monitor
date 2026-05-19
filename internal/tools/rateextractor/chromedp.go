package rateextractor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
)

const (
	defaultChromedpTimeout       = 30 * time.Second
	defaultChromedpNetworkIdleMs = 5000
)

// ChromedpRateExtractor renders pages using a headless Chrome instance, then
// applies the source's extraction rule pipeline and persists the resulting rate
// value. Each Run call spawns a fresh Chromium subprocess; callers that need
// high-throughput collection should consider a future browser-pool plan.
//
// The constructor is lazy-friendly: pass an empty chromiumPath to let chromedp
// fall back to its own PATH lookup (chromium, chromium-browser, google-chrome, chrome).
type ChromedpRateExtractor struct {
	chromiumPath string
	logger       io.Writer
	repo         rateValueRepository
	failedURLs   map[string]error
	failedURLsMu sync.Mutex
}

// NewChromedpRateExtractor constructs a ChromedpRateExtractor. chromiumPath may
// be empty, in which case chromedp searches PATH for a suitable binary. logger
// receives one-line diagnostic messages per fetch; pass io.Discard to silence them.
// Caller must supply a non-nil repo.
//
// The extractor maintains a per-process negative URL cache (tombstone): once a URL fails
// inside one process, subsequent fetches in the same process short-circuit. This is designed
// for short-lived one-shot processes; do not reuse an extractor instance across cron
// invocations in a long-running daemon.
func NewChromedpRateExtractor(chromiumPath string, logger io.Writer, repo rateValueRepository) *ChromedpRateExtractor {
	if logger == nil {
		logger = io.Discard
	}
	return &ChromedpRateExtractor{
		chromiumPath: chromiumPath,
		logger:       logger,
		repo:         repo,
		failedURLs:   make(map[string]error),
	}
}

// Run renders source.URL via headless Chrome, applies all extraction rules in
// sequence, and persists the resulting rate value via the repository supplied at
// construction time. The WaitSelector from source.Options is honoured per call
// so different sources with different selectors share one extractor instance.
func (e *ChromedpRateExtractor) Run(ctx context.Context, source *domain.RateSource) error {
	payload, err := e.fetchRenderedPage(ctx, source)
	if err != nil {
		return fmt.Errorf("chromedp extractor: source %s: %w", source.Name, err)
	}
	return applyRulesAndStore(ctx, source, payload, e.repo)
}

// loadFailedURL returns the cached error for url and true if url was previously
// recorded as failed during the current process lifetime.
func (e *ChromedpRateExtractor) loadFailedURL(url string) (error, bool) {
	e.failedURLsMu.Lock()
	defer e.failedURLsMu.Unlock()
	err, ok := e.failedURLs[url]
	return err, ok
}

// recordFailedURL stores err as the tombstone for url. Subsequent fetches of url
// inside the same process short-circuit and return a wrapped form of err at replay time.
// See constructor godoc for lifetime constraint.
func (e *ChromedpRateExtractor) recordFailedURL(url string, err error) {
	e.failedURLsMu.Lock()
	defer e.failedURLsMu.Unlock()
	e.failedURLs[url] = err
}

// fetchRenderedPage navigates to source.URL with a headless Chrome instance and
// returns the post-hydration outer HTML of the document. The hard wall-clock timeout
// is 30 s. Both the browser context and the allocator context are cancelled before
// return so the Chromium subprocess is reaped even on error paths.
// Short-circuits if a prior fetch for source.URL failed during this process lifetime; see recordFailedURL.
func (e *ChromedpRateExtractor) fetchRenderedPage(ctx context.Context, source *domain.RateSource) ([]byte, error) {
	if cached, ok := e.loadFailedURL(source.URL); ok {
		_, _ = fmt.Fprintf(e.logger,
			"chromedp_extractor: short-circuit url=%s prior_error=%v\n", source.URL, cached)
		err := fmt.Errorf("short-circuit (tombstoned this run): %w", cached)
		err = errors.Join(err, internal.NewTraceError())
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, defaultChromedpTimeout)
	defer cancel()

	allocatorOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Headless,
		chromedp.DisableGPU,
		// NoSandbox is required when Chrome runs as root (systemd unit on the
		// ARM deploy host) or inside a container.
		chromedp.NoSandbox,
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	)
	if e.chromiumPath != "" {
		allocatorOpts = append(allocatorOpts, chromedp.ExecPath(e.chromiumPath))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocatorOpts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	waitSelector := strings.TrimSpace(source.Options.WaitSelector)

	actions := []chromedp.Action{
		chromedp.Navigate(source.URL),
		chromedp.WaitVisible("body", chromedp.ByQuery),
	}
	if waitSelector != "" {
		actions = append(actions, chromedp.WaitVisible(waitSelector, chromedp.ByQuery))
	} else {
		actions = append(actions, chromedp.Sleep(time.Duration(defaultChromedpNetworkIdleMs)*time.Millisecond))
	}
	var rendered string
	actions = append(actions, chromedp.OuterHTML("html", &rendered, chromedp.ByQuery))

	if err := chromedp.Run(browserCtx, actions...); err != nil {
		wrapped := fmt.Errorf("chromedp fetch %s: %w", source.URL, err)
		e.recordFailedURL(source.URL, wrapped)
		return nil, wrapped
	}

	_, _ = fmt.Fprintf(e.logger, "chromedp_extractor: url=%s wait_selector=%q rendered_bytes=%d\n",
		source.URL, waitSelector, len(rendered))

	return []byte(rendered), nil
}
