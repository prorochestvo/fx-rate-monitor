package ruledoctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
)

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com/v1"
	defaultAnthropicVersion = "2023-06-01"
)

// AnthropicClient is a minimal HTTP client for Anthropic's /v1/messages endpoint.
// It exists alongside OllamaClient so the integration test can benchmark a strong
// hosted model (Haiku 4.5) as a quality ceiling for the LLM-extraction hypothesis.
type AnthropicClient struct {
	APIKey  string
	Model   string
	BaseURL string
	Version string
	HTTP    *http.Client
}

// NewAnthropicClient returns a client with sensible defaults. timeout applies to
// a single Generate call.
func NewAnthropicClient(apiKey, model string, timeout time.Duration) *AnthropicClient {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &AnthropicClient{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: defaultAnthropicBaseURL,
		Version: defaultAnthropicVersion,
		HTTP:    &http.Client{Timeout: timeout},
	}
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type anthropicResponse struct {
	Type    string                  `json:"type"`
	Content []anthropicContentBlock `json:"content"`
	Error   *anthropicErrorBody     `json:"error,omitempty"`
}

// Generate sends one user message and returns the concatenated text content of
// the response. Temperature is forced to 0 for determinism.
func (c *AnthropicClient) Generate(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(anthropicRequest{
		Model:       c.Model,
		MaxTokens:   1024,
		Temperature: 0.0,
		Messages: []anthropicMessage{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", errors.Join(fmt.Errorf("marshal request: %w", err), internal.NewTraceError())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return "", errors.Join(fmt.Errorf("new request: %w", err), internal.NewTraceError())
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", c.Version)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", errors.Join(fmt.Errorf("anthropic call failed: %w", err), internal.NewTraceError())
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, readErr := io.ReadAll(resp.Body)
		msg := string(raw)
		if readErr != nil {
			msg = readErr.Error()
		}
		return "", errors.Join(fmt.Errorf("anthropic returned %d: %s", resp.StatusCode, msg), internal.NewTraceError())
	}

	var out anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", errors.Join(fmt.Errorf("decode response: %w", err), internal.NewTraceError())
	}
	if out.Error != nil {
		return "", errors.Join(fmt.Errorf("anthropic error %s: %s", out.Error.Type, out.Error.Message), internal.NewTraceError())
	}

	var text string
	for _, blk := range out.Content {
		if blk.Type == "text" {
			text += blk.Text
		}
	}
	return text, nil
}
