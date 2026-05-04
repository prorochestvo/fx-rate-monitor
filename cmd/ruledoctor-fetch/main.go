// Command ruledoctor-fetch renders a URL with a headless Chrome (via chromedp)
// and writes the resulting DOM to disk. It exists to produce fixtures for the
// ruledoctor extraction tests when a target page renders its rate values via
// JavaScript and is therefore unreachable with a plain http.Get.
//
// It optionally captures every JSON XHR/fetch response observed during the
// page load. That output is useful for designing a future "discover the
// underlying API endpoint" mode of the extractor.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func main() {
	var (
		url        = flag.String("url", "", "URL to render (required)")
		out        = flag.String("out", "-", "output path for rendered HTML; - means stdout")
		wait       = flag.Duration("wait", 4*time.Second, "fixed wait after navigation completes")
		selector   = flag.String("selector", "", "if set, wait for this CSS selector to be visible instead of fixed --wait")
		timeout    = flag.Duration("timeout", 90*time.Second, "overall timeout for the entire run")
		headed     = flag.Bool("headed", false, "show the browser window (useful for debugging)")
		userAgent  = flag.String("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36", "custom user agent")
		xhrDir     = flag.String("xhr-dir", "", "if set, capture every JSON XHR/fetch response into this directory")
		xhrIndex   = flag.String("xhr-index", "", "if set with --xhr-dir, write a JSON manifest of captured XHRs to this path")
		clickFirst = flag.String("click", "", "if set, click this CSS selector after page load and before --wait")
	)
	flag.Parse()

	if *url == "" {
		log.Fatal("--url is required")
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", !*headed),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.UserAgent(*userAgent),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, ctxCancel := chromedp.NewContext(allocCtx)
	defer ctxCancel()

	ctx, timeoutCancel := context.WithTimeout(ctx, *timeout)
	defer timeoutCancel()

	var capturer *xhrCapturer
	if *xhrDir != "" {
		if err := os.MkdirAll(*xhrDir, 0o755); err != nil {
			log.Fatalf("create xhr-dir: %v", err)
		}
		capturer = newXHRCapturer(ctx)
	}

	var html string
	actions := []chromedp.Action{
		network.Enable(),
		chromedp.Navigate(*url),
	}
	if *selector != "" {
		actions = append(actions, chromedp.WaitVisible(*selector, chromedp.ByQuery))
	}
	if *clickFirst != "" {
		actions = append(actions, chromedp.Click(*clickFirst, chromedp.ByQuery))
	}
	actions = append(actions,
		chromedp.Sleep(*wait),
		chromedp.OuterHTML("html", &html, chromedp.ByQuery),
	)

	if err := chromedp.Run(ctx, actions...); err != nil {
		log.Fatalf("chromedp run: %v", err)
	}

	if err := writeOutput(*out, html); err != nil {
		log.Fatalf("write output: %v", err)
	}

	if capturer != nil {
		if err := capturer.flush(ctx, *xhrDir, *xhrIndex); err != nil {
			log.Fatalf("flush xhr capture: %v", err)
		}
	}

	fmt.Fprintf(os.Stderr, "ruledoctor-fetch: rendered %d bytes from %s\n", len(html), *url)
}

func writeOutput(path, body string) error {
	if path == "-" {
		_, err := io.WriteString(os.Stdout, body)
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

type xhrEntry struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Status      int    `json:"status"`
	Size        int    `json:"size"`
	File        string `json:"file"`
}

// xhrCapturer subscribes to network.EventResponseReceived and records every
// response whose content-type contains "json". Bodies are fetched lazily inside
// flush(), once the page has settled, because GetResponseBody only succeeds
// after the response stream has finished.
type xhrCapturer struct {
	mu      sync.Mutex
	pending []capturedResponse
}

type capturedResponse struct {
	requestID   network.RequestID
	url         string
	contentType string
	status      int
}

func newXHRCapturer(ctx context.Context) *xhrCapturer {
	c := &xhrCapturer{}
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		e, ok := ev.(*network.EventResponseReceived)
		if !ok || e == nil || e.Response == nil {
			return
		}
		ct := e.Response.MimeType
		if !strings.Contains(strings.ToLower(ct), "json") {
			return
		}
		c.mu.Lock()
		c.pending = append(c.pending, capturedResponse{
			requestID:   e.RequestID,
			url:         e.Response.URL,
			contentType: ct,
			status:      int(e.Response.Status),
		})
		c.mu.Unlock()
	})
	return c
}

func (c *xhrCapturer) flush(ctx context.Context, dir, indexPath string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries := make([]xhrEntry, 0, len(c.pending))
	for i, p := range c.pending {
		bodyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		body, err := fetchResponseBody(bodyCtx, p.requestID)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "xhr %d: skip %s: %v\n", i, p.url, err)
			continue
		}
		filename := fmt.Sprintf("%03d.json", i)
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		entries = append(entries, xhrEntry{
			URL:         p.url,
			ContentType: p.contentType,
			Status:      p.status,
			Size:        len(body),
			File:        filename,
		})
	}

	if indexPath == "" {
		return nil
	}
	idxBytes, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath, idxBytes, 0o644)
}

func fetchResponseBody(ctx context.Context, id network.RequestID) ([]byte, error) {
	body, err := network.GetResponseBody(id).Do(ctx)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, errors.New("empty body")
	}
	return body, nil
}
