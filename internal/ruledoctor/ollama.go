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

// OllamaClient is a minimal HTTP client for Ollama's /api/generate endpoint.
// It is deliberately not abstracted behind an interface yet: this whole package
// exists to validate a hypothesis. If the hypothesis holds, a follow-up plan
// will introduce a proper LLMProvider interface.
type OllamaClient struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

// NewOllamaClient returns a client with sensible defaults.
// timeout applies to a single Generate call.
func NewOllamaClient(baseURL, model string, timeout time.Duration) *OllamaClient {
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	return &OllamaClient{
		BaseURL: baseURL,
		Model:   model,
		HTTP:    &http.Client{Timeout: timeout},
	}
}

type ollamaRequest struct {
	Model   string         `json:"model"`
	Prompt  string         `json:"prompt"`
	Stream  bool           `json:"stream"`
	Format  string         `json:"format,omitempty"`
	Options map[string]any `json:"options,omitempty"`
}

type ollamaResponse struct {
	Model    string `json:"model"`
	Response string `json:"response"`
	Done     bool   `json:"done"`
	Error    string `json:"error,omitempty"`
}

// Generate sends a single non-streaming prompt to Ollama and returns the model's
// raw response string. The caller is expected to JSON-decode it.
func (c *OllamaClient) Generate(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(ollamaRequest{
		Model:  c.Model,
		Prompt: prompt,
		Stream: false,
		Format: "json",
		Options: map[string]any{
			"temperature": 0.0,
			"num_ctx":     8192,
		},
	})
	if err != nil {
		return "", errors.Join(fmt.Errorf("marshal request: %w", err), internal.NewTraceError())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", errors.Join(fmt.Errorf("new request: %w", err), internal.NewTraceError())
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", errors.Join(fmt.Errorf("ollama call failed: %w", err), internal.NewTraceError())
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", errors.Join(fmt.Errorf("ollama returned %d: %s", resp.StatusCode, string(raw)), internal.NewTraceError())
	}

	var out ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", errors.Join(fmt.Errorf("decode response: %w", err), internal.NewTraceError())
	}
	if out.Error != "" {
		return "", errors.Join(fmt.Errorf("ollama error: %s", out.Error), internal.NewTraceError())
	}

	return out.Response, nil
}
