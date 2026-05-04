package ruledoctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/seilbekskindirov/monitor/internal"
)

// ClaudeCodeClient invokes the local `claude` CLI as a subprocess. It uses
// whatever auth Claude Code has configured (subscription via OAuth/keychain
// or API key) — no separate ANTHROPIC_API_KEY is required.
//
// We deliberately work from os.TempDir() to keep CLAUDE.md auto-discovery
// from injecting unrelated repo context into the model's prompt.
type ClaudeCodeClient struct {
	Binary       string
	Model        string // e.g. "haiku" / "sonnet" / "opus" / full id
	Effort       string // e.g. "low" / "medium" / "high" / "" (unset)
	SystemPrompt string
	WorkDir      string
	Timeout      time.Duration
}

const defaultClaudeSystemPrompt = "You are an HTML data extraction expert. Respond with exactly one JSON object — no commentary, no markdown fences."

// NewClaudeCodeClient returns a client with sensible defaults.
func NewClaudeCodeClient(model, effort string, timeout time.Duration) *ClaudeCodeClient {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	if model == "" {
		model = "haiku"
	}
	return &ClaudeCodeClient{
		Binary:       "claude",
		Model:        model,
		Effort:       effort,
		SystemPrompt: defaultClaudeSystemPrompt,
		WorkDir:      os.TempDir(),
		Timeout:      timeout,
	}
}

// Generate runs the `claude` CLI in non-interactive mode and returns its
// stdout (the model's final text). stderr is included in any error message.
func (c *ClaudeCodeClient) Generate(ctx context.Context, prompt string) (string, error) {
	args := []string{
		"--print",
		"--no-session-persistence",
		"--output-format", "text",
		"--model", c.Model,
		"--system-prompt", c.SystemPrompt,
	}
	if c.Effort != "" {
		args = append(args, "--effort", c.Effort)
	}
	args = append(args, prompt)

	cmdCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, c.Binary, args...)
	if c.WorkDir != "" {
		cmd.Dir = c.WorkDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", errors.Join(
			fmt.Errorf("claude cli failed: %w; stderr=%s", err, strings.TrimSpace(stderr.String())),
			internal.NewTraceError(),
		)
	}

	return strings.TrimSpace(stdout.String()), nil
}
