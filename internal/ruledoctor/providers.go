// Package ruledoctor provides LLM-driven extraction-rule generation for FX rate
// sources.  Adding a fifth provider requires three steps:
//
//  1. Create a new file (e.g. myprovider.go) implementing the Generator interface.
//  2. Add a new case "myprovider": branch in NewGenerator below.
//  3. Document the required env vars in ProviderConfig and/or the case comment.
package ruledoctor

import (
	"fmt"
	"time"
)

// ProviderConfig holds all parameters needed to construct any Generator.
// The caller populates only the fields relevant to the chosen Provider.
type ProviderConfig struct {
	Provider string        // "groq" | "anthropic" | "ollama" | "claudecode"
	Model    string        // provider-specific model identifier
	APIKey   string        // groq / anthropic key
	BaseURL  string        // ollama base URL
	Effort   string        // claudecode effort level (low/medium/high)
	Timeout  time.Duration // per-request timeout; <=0 uses the client's default
}

// NewGenerator returns a Generator for the named provider. It returns an
// error (never calls t.Skip) so that non-test callers can handle a missing
// credential without a panic.
func NewGenerator(cfg ProviderConfig) (Generator, error) {
	switch cfg.Provider {
	case "groq":
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("groq provider requires a non-empty APIKey (GROQ_API_KEY)")
		}
		model := cfg.Model
		if model == "" {
			model = "llama-3.1-8b-instant"
		}
		return NewGroqClient(cfg.APIKey, model, cfg.Timeout), nil

	case "anthropic":
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("anthropic provider requires a non-empty APIKey (ANTHROPIC_API_KEY)")
		}
		model := cfg.Model
		if model == "" {
			model = "claude-haiku-4-5-20251001"
		}
		return NewAnthropicClient(cfg.APIKey, model, cfg.Timeout), nil

	case "ollama":
		if cfg.BaseURL == "" {
			return nil, fmt.Errorf("ollama provider requires a non-empty BaseURL (OLLAMA_URL)")
		}
		model := cfg.Model
		if model == "" {
			model = "qwen2.5:1.5b-instruct"
		}
		return NewOllamaClient(cfg.BaseURL, model, cfg.Timeout), nil

	case "claudecode":
		model := cfg.Model
		if model == "" {
			model = "haiku"
		}
		return NewClaudeCodeClient(model, cfg.Effort, cfg.Timeout), nil

	default:
		return nil, fmt.Errorf("unknown provider %q (use groq | anthropic | ollama | claudecode)", cfg.Provider)
	}
}
