package ruledoctor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/seilbekskindirov/monitor/internal/ruledoctor"
)

type rowResult struct {
	source   string
	pair     string
	expected string
	got      string
	ex       *ruledoctor.Extraction
	vr       ruledoctor.VerifyResult
	callErr  error
	parseErr error
	dur      time.Duration
}

const (
	envProvider     = "RULEDOCTOR_PROVIDER" // "ollama" | "anthropic" | "claudecode" | "groq"
	envOllamaURL    = "OLLAMA_URL"
	envAnthropicKey = "ANTHROPIC_API_KEY"
	envGroqKey      = "GROQ_API_KEY"
	envModel        = "RULEDOCTOR_MODEL"
	envEffort       = "RULEDOCTOR_EFFORT" // claudecode only
	envLimit        = "RULEDOCTOR_LIMIT"
	envSource       = "RULEDOCTOR_SOURCE" // optional: filter to a single fixture by name
	envTimeout      = "RULEDOCTOR_TIMEOUT"

	defaultOllamaModel     = "qwen2.5:1.5b-instruct"
	defaultAnthropicModel  = "claude-haiku-4-5-20251001"
	defaultClaudeCodeModel = "haiku"
	defaultGroqModel       = "llama-3.3-70b-versatile"

	providerOllama     = "ollama"
	providerAnthropic  = "anthropic"
	providerClaudeCode = "claudecode"
	providerGroq       = "groq"

	fixtureSuffix    = ".html"
	expectedSuffixGo = "_expected.json"
)

type expectedFile struct {
	Pairs []expectedPair `json:"pairs"`
}

type expectedPair struct {
	Pair  string `json:"pair"`
	Unit  int    `json:"unit"`
	Value string `json:"value"`
}

type fixture struct {
	name     string
	htmlPath string
	expected expectedFile
}

// TestExtract is an integration test that exercises the LLM-extraction hypothesis
// across every fixture in testdata/ruledoctor/. A fixture is a pair of files:
// `<name>.html` (the original page, possibly pre-rendered via cmd/ruledoctor-fetch)
// and `<name>_expected.json` (the verified pair → value mapping).
//
// It is skipped unless the relevant credential env var is set.
//
// Configuration (env):
//   - RULEDOCTOR_PROVIDER ("ollama" | "anthropic" | "claudecode" | "groq")
//   - OLLAMA_URL / ANTHROPIC_API_KEY / GROQ_API_KEY (per provider)
//   - RULEDOCTOR_MODEL    (default per provider)
//   - RULEDOCTOR_LIMIT    (default 0 = all pairs across all fixtures)
//   - RULEDOCTOR_SOURCE   (optional: filter to a single fixture by name, e.g. "bcc")
//   - RULEDOCTOR_TIMEOUT  (per-request)
func TestExtract(t *testing.T) {
	provider := os.Getenv(envProvider)
	if provider == "" {
		provider = providerOllama
	}

	model := os.Getenv(envModel)
	client, timeout := newClient(t, provider, model)

	fixtures := loadFixtures(t)
	if filter := os.Getenv(envSource); filter != "" {
		fixtures = filterFixtures(fixtures, filter)
		require.NotEmpty(t, fixtures, "no fixture matched %s=%q", envSource, filter)
	}

	limit := 0
	if v := os.Getenv(envLimit); v != "" {
		n, err := strconv.Atoi(v)
		require.NoError(t, err, "%s must be an integer", envLimit)
		limit = n
	}

	var (
		results []rowResult
		mu      sync.Mutex
	)

	for _, f := range fixtures {
		htmlBytes, err := os.ReadFile(f.htmlPath)
		require.NoError(t, err, "read %s", f.htmlPath)
		originalHTML := string(htmlBytes)
		cleanedHTML := ruledoctor.Clean(originalHTML)
		t.Logf("fixture %q: original=%d bytes, cleaned=%d bytes, pairs=%d",
			f.name, len(originalHTML), len(cleanedHTML), len(f.expected.Pairs))

		pairs := f.expected.Pairs
		if limit > 0 && limit < len(pairs) {
			pairs = pairs[:limit]
		}

		t.Run(f.name, func(t *testing.T) {
			for i, p := range pairs {
				p := p
				t.Run(fmt.Sprintf("%02d_%s", i+1, sanitize(p.Pair)), func(t *testing.T) {
					row := runOnePair(t, client, timeout, originalHTML, cleanedHTML, f.name, p)
					mu.Lock()
					results = append(results, row)
					mu.Unlock()
				})
			}
		})
	}

	t.Cleanup(func() {
		printSummary(t, results)
	})
}

func runOnePair(
	t *testing.T,
	client ruledoctor.Generator,
	timeout time.Duration,
	originalHTML, cleanedHTML, source string,
	p expectedPair,
) rowResult {
	t.Helper()

	snippet := ruledoctor.SnipForPair(cleanedHTML, p.Pair)
	require.NotEmpty(t, snippet, "snipper failed to locate pair %q in cleaned HTML", p.Pair)
	t.Logf("snippet: %d bytes", len(snippet))

	prompt := ruledoctor.BuildPrompt(snippet, p.Pair)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	raw, err := client.Generate(ctx, prompt)
	dur := time.Since(start)

	row := rowResult{source: source, pair: p.Pair, expected: p.Value, dur: dur}
	if err != nil {
		row.callErr = err
		t.Errorf("llm call failed in %s: %v", dur, err)
		return row
	}
	row.got = raw

	ex, err := ruledoctor.ParseExtraction(raw)
	if err != nil {
		row.parseErr = err
		t.Errorf("parse extraction in %s: %v\nraw=%s", dur, err, raw)
		return row
	}
	row.ex = ex

	vr := ruledoctor.Verify(originalHTML, p.Value, ex)
	row.vr = vr

	t.Logf(
		"value=%q (match=%v) css=%q (match=%v err=%v) regex=%q (match=%v err=%v) conf=%.2f dur=%s",
		ex.Value, vr.ValueMatches,
		ex.CSSSelector, vr.CSSMatches, errStr(vr.CSSError),
		ex.Regex, vr.RegexMatches, errStr(vr.RegexError),
		ex.Confidence, dur,
	)

	if !vr.ValueMatches {
		t.Errorf("value mismatch: expected %q, got %q", p.Value, ex.Value)
	}
	return row
}

func newClient(t *testing.T, provider, model string) (ruledoctor.Generator, time.Duration) {
	t.Helper()

	// Per-provider skip guards and timeout selection live here, not in
	// NewGenerator, so that the production helper stays t.Skip-free.
	switch provider {
	case providerOllama:
		baseURL := os.Getenv(envOllamaURL)
		if baseURL == "" {
			t.Skipf("set %s to run with provider=ollama", envOllamaURL)
		}
		if model == "" {
			model = defaultOllamaModel
		}
		timeout := resolveTimeout(t, 180*time.Second)
		t.Logf("provider=ollama model=%s url=%s timeout=%s", model, baseURL, timeout)
		g, err := ruledoctor.NewGenerator(ruledoctor.ProviderConfig{
			Provider: "ollama", Model: model, BaseURL: baseURL, Timeout: timeout,
		})
		require.NoError(t, err)
		return g, timeout

	case providerAnthropic:
		apiKey := os.Getenv(envAnthropicKey)
		if apiKey == "" {
			t.Skipf("set %s to run with provider=anthropic", envAnthropicKey)
		}
		if model == "" {
			model = defaultAnthropicModel
		}
		timeout := resolveTimeout(t, 60*time.Second)
		t.Logf("provider=anthropic model=%s timeout=%s", model, timeout)
		g, err := ruledoctor.NewGenerator(ruledoctor.ProviderConfig{
			Provider: "anthropic", Model: model, APIKey: apiKey, Timeout: timeout,
		})
		require.NoError(t, err)
		return g, timeout

	case providerClaudeCode:
		if model == "" {
			model = defaultClaudeCodeModel
		}
		effort := os.Getenv(envEffort)
		timeout := resolveTimeout(t, 120*time.Second)
		t.Logf("provider=claudecode model=%s effort=%q timeout=%s", model, effort, timeout)
		g, err := ruledoctor.NewGenerator(ruledoctor.ProviderConfig{
			Provider: "claudecode", Model: model, Effort: effort, Timeout: timeout,
		})
		require.NoError(t, err)
		return g, timeout

	case providerGroq:
		apiKey := os.Getenv(envGroqKey)
		if apiKey == "" {
			t.Skipf("set %s to run with provider=groq", envGroqKey)
		}
		if model == "" {
			model = defaultGroqModel
		}
		timeout := resolveTimeout(t, 60*time.Second)
		t.Logf("provider=groq model=%s timeout=%s", model, timeout)
		g, err := ruledoctor.NewGenerator(ruledoctor.ProviderConfig{
			Provider: "groq", Model: model, APIKey: apiKey, Timeout: timeout,
		})
		require.NoError(t, err)
		return g, timeout

	default:
		t.Fatalf("unknown %s=%q (use %q | %q | %q | %q)", envProvider, provider,
			providerOllama, providerAnthropic, providerClaudeCode, providerGroq)
		return nil, 0
	}
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()

	dir := filepath.Join("..", "..", "testdata", "ruledoctor")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err, "read %s", dir)

	var fixtures []fixture
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), fixtureSuffix) {
			continue
		}
		name := strings.TrimSuffix(e.Name(), fixtureSuffix)
		htmlPath := filepath.Join(dir, e.Name())
		expectedPath := filepath.Join(dir, name+expectedSuffixGo)

		expBytes, err := os.ReadFile(expectedPath)
		if err != nil {
			t.Logf("skipping fixture %q: missing %s", name, expectedPath)
			continue
		}
		var exp expectedFile
		require.NoError(t, json.Unmarshal(expBytes, &exp), "decode %s", expectedPath)
		require.NotEmpty(t, exp.Pairs, "%s has no pairs", expectedPath)

		fixtures = append(fixtures, fixture{name: name, htmlPath: htmlPath, expected: exp})
	}

	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].name < fixtures[j].name })
	require.NotEmpty(t, fixtures, "no fixtures found in %s", dir)
	return fixtures
}

func filterFixtures(fixtures []fixture, want string) []fixture {
	out := make([]fixture, 0, len(fixtures))
	for _, f := range fixtures {
		if f.name == want {
			out = append(out, f)
		}
	}
	return out
}

func resolveTimeout(t *testing.T, fallback time.Duration) time.Duration {
	t.Helper()
	v := os.Getenv(envTimeout)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	require.NoError(t, err, "%s must be a Go duration", envTimeout)
	return d
}

func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

func errStr(e error) string {
	if e == nil {
		return "<nil>"
	}
	return e.Error()
}

func printSummary(t *testing.T, results []rowResult) {
	t.Helper()
	if len(results) == 0 {
		return
	}

	type bucket struct {
		valueOK, cssOK, regexOK int
		callFail, parseFail     int
		total                   int
		totalDur                time.Duration
	}
	per := map[string]*bucket{}
	overall := &bucket{}

	for _, r := range results {
		b := per[r.source]
		if b == nil {
			b = &bucket{}
			per[r.source] = b
		}
		b.total++
		overall.total++
		b.totalDur += r.dur
		overall.totalDur += r.dur
		if r.callErr != nil {
			b.callFail++
			overall.callFail++
			continue
		}
		if r.parseErr != nil {
			b.parseFail++
			overall.parseFail++
			continue
		}
		if r.vr.ValueMatches {
			b.valueOK++
			overall.valueOK++
		}
		if r.vr.CSSMatches {
			b.cssOK++
			overall.cssOK++
		}
		if r.vr.RegexMatches {
			b.regexOK++
			overall.regexOK++
		}
	}

	names := make([]string, 0, len(per))
	for n := range per {
		names = append(names, n)
	}
	sort.Strings(names)

	t.Logf("======== ruledoctor summary ========")
	for _, n := range names {
		b := per[n]
		t.Logf("[%s] pairs=%d value=%d/%d (%.0f%%) css=%d/%d (%.0f%%) regex=%d/%d (%.0f%%) call_err=%d parse_err=%d total=%s avg=%s/pair",
			n, b.total,
			b.valueOK, b.total, pct(b.valueOK, b.total),
			b.cssOK, b.total, pct(b.cssOK, b.total),
			b.regexOK, b.total, pct(b.regexOK, b.total),
			b.callFail, b.parseFail,
			b.totalDur, avgDur(b.totalDur, b.total),
		)
	}
	t.Logf("[OVERALL] pairs=%d value=%d/%d (%.0f%%) css=%d/%d (%.0f%%) regex=%d/%d (%.0f%%) call_err=%d parse_err=%d total=%s avg=%s/pair",
		overall.total,
		overall.valueOK, overall.total, pct(overall.valueOK, overall.total),
		overall.cssOK, overall.total, pct(overall.cssOK, overall.total),
		overall.regexOK, overall.total, pct(overall.regexOK, overall.total),
		overall.callFail, overall.parseFail,
		overall.totalDur, avgDur(overall.totalDur, overall.total),
	)
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(n) / float64(total)
}

func avgDur(d time.Duration, n int) time.Duration {
	if n == 0 {
		return 0
	}
	return d / time.Duration(n)
}
