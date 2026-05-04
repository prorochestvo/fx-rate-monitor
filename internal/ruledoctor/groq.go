package ruledoctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
)

// reGroqRetry extracts the "Please try again in X.XXs" hint Groq returns in
// 429 responses so we can sleep precisely instead of guessing.
var reGroqRetry = regexp.MustCompile(`try again in ([0-9.]+)s`)

const groqMaxRetries = 4

const defaultGroqBaseURL = "https://api.groq.com/openai/v1"

// GroqClient calls Groq's OpenAI-compatible /chat/completions endpoint.
// Free tier (no card) gives ~14400 req/day on the small Llama models — useful
// as a cheap-or-free production candidate compared to Haiku.
type GroqClient struct {
	APIKey  string
	Model   string
	BaseURL string
	HTTP    *http.Client
}

// NewGroqClient returns a client with sensible defaults. timeout applies to a
// single Generate call.
func NewGroqClient(apiKey, model string, timeout time.Duration) *GroqClient {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &GroqClient{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: defaultGroqBaseURL,
		HTTP:    &http.Client{Timeout: timeout},
	}
}

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqRequest struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Messages    []groqMessage `json:"messages"`
}

type groqChoice struct {
	Message groqMessage `json:"message"`
}

type groqErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type groqResponse struct {
	Choices []groqChoice   `json:"choices"`
	Error   *groqErrorBody `json:"error,omitempty"`
}

// Generate sends one user message and returns the first choice's text. Temperature
// is forced to 0 for determinism. On HTTP 429 (rate limit) it parses Groq's
// "try again in X.XXs" hint and retries up to groqMaxRetries times, capped by
// the caller's context deadline.
func (c *GroqClient) Generate(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(groqRequest{
		Model:       c.Model,
		Temperature: 0.0,
		MaxTokens:   1024,
		Messages: []groqMessage{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", errors.Join(fmt.Errorf("marshal request: %w", err), internal.NewTraceError())
	}

	for attempt := 0; ; attempt++ {
		text, retryAfter, err := c.doOnce(ctx, body)
		if err == nil {
			return text, nil
		}
		if retryAfter <= 0 || attempt >= groqMaxRetries {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", errors.Join(ctx.Err(), err)
		case <-time.After(retryAfter):
		}
	}
}

// doOnce performs a single request. retryAfter > 0 indicates the caller should
// sleep that long and try again; otherwise the error is final.
func (c *GroqClient) doOnce(ctx context.Context, body []byte) (string, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", 0, errors.Join(fmt.Errorf("new request: %w", err), internal.NewTraceError())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", 0, errors.Join(fmt.Errorf("groq call failed: %w", err), internal.NewTraceError())
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, readErr := io.ReadAll(resp.Body)
		msg := string(raw)
		if readErr != nil {
			msg = readErr.Error()
		}
		err := errors.Join(fmt.Errorf("groq returned %d: %s", resp.StatusCode, msg), internal.NewTraceError())
		if resp.StatusCode == http.StatusTooManyRequests {
			return "", parseGroqRetryAfter(resp.Header, msg), err
		}
		return "", 0, err
	}

	var out groqResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", 0, errors.Join(fmt.Errorf("decode response: %w", err), internal.NewTraceError())
	}
	if out.Error != nil {
		return "", 0, errors.Join(fmt.Errorf("groq error %s: %s", out.Error.Type, out.Error.Message), internal.NewTraceError())
	}
	if len(out.Choices) == 0 {
		return "", 0, errors.Join(errors.New("groq returned no choices"), internal.NewTraceError())
	}
	return out.Choices[0].Message.Content, 0, nil
}

// parseGroqRetryAfter prefers the "Please try again in X.XXs" hint embedded in
// Groq's error body because it is more precise than the integer-second
// Retry-After header. Adds a small jitter so concurrent callers don't all
// wake up at exactly the same instant.
func parseGroqRetryAfter(h http.Header, body string) time.Duration {
	if m := reGroqRetry.FindStringSubmatch(body); len(m) == 2 {
		if secs, err := strconv.ParseFloat(m[1], 64); err == nil {
			return time.Duration(secs*1000)*time.Millisecond + 200*time.Millisecond
		}
	}
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil {
			return time.Duration(secs)*time.Second + 200*time.Millisecond
		}
	}
	return 2 * time.Second
}
