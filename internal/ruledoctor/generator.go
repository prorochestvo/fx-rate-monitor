package ruledoctor

import "context"

// Generator is the minimal interface used by the integration test to call any
// LLM backend. Both OllamaClient and AnthropicClient satisfy it. Keeping this
// abstraction tiny on purpose — this whole package is a hypothesis test, not
// production plumbing.
type Generator interface {
	Generate(ctx context.Context, prompt string) (string, error)
}
