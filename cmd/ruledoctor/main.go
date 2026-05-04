// cmd/ruledoctor is a manual-run CLI for generating and managing LLM-driven
// extraction rules.  It runs on the developer's Mac (not on the prod VPS) and
// writes a self-contained SQL artifact that the user reviews and applies via
// `sqlite3 build/monitor.db < <file>`.
//
// Subcommands:
//
//	generate --target=<id>[,<id>...]  Generate rules for the named targets.
//	regenerate-broken                 Re-generate rules for targets whose active rule is marked broken.
//	list-rules [--target=<id>]        Print active rules (read-only).
//
// Configuration is via environment variables (consistent with the collector/notifier):
//
//	RULEDOCTOR_PROVIDER   groq (default) | anthropic | ollama | claudecode
//	RULEDOCTOR_MODEL      provider-specific model id
//	GROQ_API_KEY          required for provider=groq
//	ANTHROPIC_API_KEY     required for provider=anthropic
//	OLLAMA_URL            required for provider=ollama
//	RULEDOCTOR_TIMEOUT    per-request timeout (Go duration, default 60s)
//	RULEDOCTOR_OUT_DIR    where SQL artifacts land (default ./tmp/ruledoctor-out/)
//	SQLITEDB_DSN          SQLite DSN — used only by regenerate-broken / list-rules
//	PROXY_URL             optional HTTP/SOCKS5 proxy
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prorochestvo/dsninjector"
	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	"github.com/seilbekskindirov/monitor/internal/repository"
	"github.com/seilbekskindirov/monitor/internal/ruledoctor"
	"github.com/twinj/uuid"
	_ "modernc.org/sqlite"
)

const (
	envProvider     = "RULEDOCTOR_PROVIDER"
	envModel        = "RULEDOCTOR_MODEL"
	envGroqKey      = "GROQ_API_KEY"
	envAnthropicKey = "ANTHROPIC_API_KEY"
	envOllamaURL    = "OLLAMA_URL"
	envEffort       = "RULEDOCTOR_EFFORT"
	envTimeout      = "RULEDOCTOR_TIMEOUT"
	envOutDir       = "RULEDOCTOR_OUT_DIR"
	envDsnSqliteDB  = "SQLITEDB_DSN"
	envProxyURL     = "PROXY_URL"

	defaultProvider = "groq"
	defaultModel    = "llama-3.1-8b-instant"
	defaultTimeout  = 60 * time.Second
	defaultOutDir   = "./tmp/ruledoctor-out/"
	defaultTargets  = "./configs/ruledoctor-targets.json"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		log.Fatal("usage: ruledoctor <generate|regenerate-broken|list-rules> [flags]")
	}

	var exitCode int
	var runErr error

	switch os.Args[1] {
	case "generate":
		exitCode, runErr = runGenerate(os.Args[2:])
	case "regenerate-broken":
		exitCode, runErr = runRegenerateBroken(os.Args[2:])
	case "list-rules":
		exitCode, runErr = runListRules(os.Args[2:])
	default:
		log.Fatalf("unknown subcommand %q; use generate | regenerate-broken | list-rules", os.Args[1])
	}

	if runErr != nil {
		log.Printf("error: %v", runErr)
	}
	os.Exit(exitCode)
}

func runGenerate(args []string) (int, error) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	targetFlag := fs.String("target", "", "comma-separated target IDs to generate rules for")
	targetsFile := fs.String("targets-file", defaultTargets, "path to the JSON targets file")
	if err := fs.Parse(args); err != nil {
		return 1, err
	}

	targets, err := loadTargets(*targetsFile)
	if err != nil {
		return 1, err
	}

	if *targetFlag != "" {
		filter := splitTrim(*targetFlag)
		targets = filterTargets(targets, filter)
	}
	if len(targets) == 0 {
		return 1, fmt.Errorf("no matching targets found in %s", *targetsFile)
	}

	gen, timeout, err := buildGenerator()
	if err != nil {
		return 1, err
	}

	return generateRules(targets, gen, timeout)
}

func runRegenerateBroken(args []string) (int, error) {
	fs := flag.NewFlagSet("regenerate-broken", flag.ExitOnError)
	targetsFile := fs.String("targets-file", defaultTargets, "path to the JSON targets file")
	if err := fs.Parse(args); err != nil {
		return 1, err
	}

	ruleRepo, db, err := buildRuleRepo()
	if err != nil {
		return 1, err
	}
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()

	ctx := context.Background()
	brokenIDs, err := ruleRepo.ObtainBrokenTargets(ctx, domain.ExtractionRuleKindRate)
	if err != nil {
		return 1, fmt.Errorf("obtain broken targets: %w", err)
	}
	if len(brokenIDs) == 0 {
		log.Println("no broken targets found")
		return 0, nil
	}

	log.Printf("found %d broken target(s): %s", len(brokenIDs), strings.Join(brokenIDs, ", "))

	allTargets, err := loadTargets(*targetsFile)
	if err != nil {
		return 1, err
	}
	targets := filterTargets(allTargets, brokenIDs)
	if len(targets) == 0 {
		return 1, fmt.Errorf("broken targets %v not found in %s — update the targets file", brokenIDs, *targetsFile)
	}

	gen, timeout, err := buildGenerator()
	if err != nil {
		return 1, err
	}

	return generateRules(targets, gen, timeout)
}

func runListRules(args []string) (int, error) {
	fs := flag.NewFlagSet("list-rules", flag.ExitOnError)
	targetFlag := fs.String("target", "", "filter to a single target ID")
	if err := fs.Parse(args); err != nil {
		return 1, err
	}

	ruleRepo, db, err := buildRuleRepo()
	if err != nil {
		return 1, err
	}
	defer func() {
		if db != nil {
			_ = db.Close()
		}
	}()

	ctx := context.Background()
	var rules []domain.ExtractionRule

	if *targetFlag != "" {
		rules, err = ruleRepo.ObtainAllRulesByTarget(ctx, domain.ExtractionRuleKindRate, *targetFlag)
	} else {
		rules, err = ruleRepo.ObtainAllActiveRules(ctx, domain.ExtractionRuleKindRate)
	}
	if err != nil {
		return 1, err
	}

	fmt.Printf("%-30s  %-20s  %-10s  %-40s  %-20s  %s\n",
		"target_id", "label", "method", "provider_tag", "generated_at", "status")
	for _, r := range rules {
		fmt.Printf("%-30s  %-20s  %-10s  %-40s  %-20s  %s\n",
			r.TargetID,
			r.Label,
			string(r.Method),
			r.ProviderTag,
			r.GeneratedAt.Format(time.RFC3339),
			string(r.Status),
		)
	}
	return 0, nil
}

// targetEntry is one item in the JSON targets file.
type targetEntry struct {
	TargetKind string   `json:"target_kind"`
	TargetID   string   `json:"target_id"`
	SourceURL  string   `json:"source_url"`
	Labels     []string `json:"labels"`
}

func loadTargets(path string) ([]targetEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("targets file not found at %s — create it first (see plan/README)", path)
		}
		return nil, fmt.Errorf("open targets file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var targets []targetEntry
	if err = dec.Decode(&targets); err != nil {
		return nil, fmt.Errorf("parse targets file %s: %w", path, err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("targets file %s is empty", path)
	}
	return targets, nil
}

func filterTargets(all []targetEntry, ids []string) []targetEntry {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	out := make([]targetEntry, 0, len(ids))
	for _, t := range all {
		if set[t.TargetID] {
			out = append(out, t)
		}
	}
	return out
}

type generateResult struct {
	targetID string
	label    string
	ruleID   string
	rule     *domain.ExtractionRule
	ex       *ruledoctor.Extraction
	vr       ruledoctor.VerifyResult
	skipped  bool
	reason   string
}

func generateRules(targets []targetEntry, gen ruledoctor.Generator, timeout time.Duration) (int, error) {
	provider := resolveEnv(envProvider, defaultProvider)
	model := resolveEnv(envModel, defaultModel)
	providerTag := fmt.Sprintf("ruledoctor:%s:%s", provider, model)

	outDir := resolveEnv(envOutDir, defaultOutDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return 1, fmt.Errorf("create out dir %s: %w", outDir, err)
	}

	now := time.Now().UTC()
	sqlPath, reportPath, err := artifactPaths(outDir, now)
	if err != nil {
		return 1, err
	}

	sqlF, err := os.Create(sqlPath)
	if err != nil {
		return 1, fmt.Errorf("create sql artifact %s: %w", sqlPath, err)
	}
	defer func() { _ = sqlF.Close() }()

	reportF, err := os.Create(reportPath)
	if err != nil {
		return 1, fmt.Errorf("create report %s: %w", reportPath, err)
	}
	defer func() { _ = reportF.Close() }()

	targetIDs := make([]string, 0, len(targets))
	for _, t := range targets {
		targetIDs = append(targetIDs, t.TargetID)
	}

	writeHeader(sqlF, now, providerTag, targetIDs)
	_, _ = fmt.Fprintf(sqlF, "\nBEGIN TRANSACTION;\n")

	proxyURL := os.Getenv(envProxyURL)
	httpClient := buildHTTPClient(proxyURL, timeout)

	var results []generateResult
	anySkipped := false

	for _, target := range targets {
		htmlBytes, fetchErr := fetchURL(httpClient, target.SourceURL)
		if fetchErr != nil {
			log.Printf("WARN: fetch %s (%s): %v — skipping all labels", target.TargetID, target.SourceURL, fetchErr)
			for _, label := range target.Labels {
				results = append(results, generateResult{
					targetID: target.TargetID,
					label:    label,
					skipped:  true,
					reason:   fmt.Sprintf("fetch failed: %v", fetchErr),
				})
				anySkipped = true
			}
			continue
		}
		originalHTML := string(htmlBytes)
		cleanedHTML := ruledoctor.Clean(originalHTML)
		contextHash := sha256Hex(cleanedHTML)

		for _, label := range target.Labels {
			snippet := ruledoctor.SnipForPair(cleanedHTML, label)
			if snippet == "" {
				log.Printf("WARN: %s label %q not found in HTML — skipping", target.TargetID, label)
				results = append(results, generateResult{
					targetID: target.TargetID,
					label:    label,
					skipped:  true,
					reason:   "label not found in HTML",
				})
				anySkipped = true
				continue
			}

			prompt := ruledoctor.BuildPrompt(snippet, label)
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			raw, genErr := gen.Generate(ctx, prompt)
			cancel()
			if genErr != nil {
				log.Printf("WARN: %s label %q: LLM call failed: %v — skipping", target.TargetID, label, genErr)
				results = append(results, generateResult{
					targetID: target.TargetID,
					label:    label,
					skipped:  true,
					reason:   fmt.Sprintf("LLM call failed: %v", genErr),
				})
				anySkipped = true
				continue
			}

			ex, parseErr := ruledoctor.ParseExtraction(raw)
			if parseErr != nil {
				log.Printf("WARN: %s label %q: parse extraction failed: %v — skipping", target.TargetID, label, parseErr)
				results = append(results, generateResult{
					targetID: target.TargetID,
					label:    label,
					skipped:  true,
					reason:   fmt.Sprintf("parse extraction failed: %v", parseErr),
				})
				anySkipped = true
				continue
			}

			vr := ruledoctor.Verify(originalHTML, ex.Value, ex)

			passed := vr.ValueMatches && (vr.CSSMatches || vr.RegexMatches)

			ruleID := generateRuleID(now)
			method, pattern := bestRule(ex, vr)

			r := generateResult{
				targetID: target.TargetID,
				label:    label,
				ruleID:   ruleID,
				rule: &domain.ExtractionRule{
					ID:          ruleID,
					TargetKind:  domain.ExtractionRuleKind(target.TargetKind),
					TargetID:    target.TargetID,
					Label:       label,
					SourceURL:   target.SourceURL,
					Method:      method,
					Pattern:     pattern,
					ProviderTag: providerTag,
					ContextHash: contextHash,
					Status:      domain.ExtractionRuleStatusActive,
					GeneratedAt: now,
					Notes:       fmt.Sprintf("verified css=%v regex=%v", vr.CSSMatches, vr.RegexMatches),
				},
				ex:      ex,
				vr:      vr,
				skipped: !passed,
			}
			if !passed {
				r.reason = verifyFailReason(vr)
				anySkipped = true
			}
			results = append(results, r)
		}
	}

	emitSQL(sqlF, results, now)
	_, _ = fmt.Fprintf(sqlF, "\nCOMMIT;\n")

	emitReport(reportF, results, providerTag)

	log.Printf("SQL artifact: %s", sqlPath)
	log.Printf("Report:       %s", reportPath)

	if anySkipped {
		return 1, nil
	}
	return 0, nil
}

func buildGenerator() (ruledoctor.Generator, time.Duration, error) {
	provider := resolveEnv(envProvider, defaultProvider)
	model := resolveEnv(envModel, defaultModel)
	timeout := resolveTimeout()

	cfg := ruledoctor.ProviderConfig{
		Provider: provider,
		Model:    model,
		APIKey:   os.Getenv(envGroqKey),
		Timeout:  timeout,
	}
	if provider == "anthropic" {
		cfg.APIKey = os.Getenv(envAnthropicKey)
	}
	cfg.BaseURL = os.Getenv(envOllamaURL)
	cfg.Effort = os.Getenv(envEffort)

	gen, err := ruledoctor.NewGenerator(cfg)
	if err != nil {
		return nil, 0, fmt.Errorf("build generator: %w", err)
	}
	return gen, timeout, nil
}

// buildRuleRepo opens the SQLite database and constructs the extraction-rule
// repository for the read-only CLI subcommands (regenerate-broken, list-rules).
// This binary never writes rule data — all rule changes flow through the SQL
// artifact the user reviews and applies manually (see plan 003).
//
// The read-only guarantee is STRUCTURAL, not enforced by SQLite. Reasons:
//   - PRAGMA query_only is a per-connection setting; database/sql pools up to
//     7 connections, so a pragma applied via one of them does not bind the
//     others.
//   - NewExtractionRuleRepository runs the migrator, which on a fresh DB
//     issues CREATE TABLE — itself a write.
//
// What actually keeps this safe is that no CLI subcommand calls any of
// RetainExtractionRule, InstallActiveRule, MarkRuleStatus, or TouchVerifiedAt.
// If you add a subcommand here, preserve that invariant.
func buildRuleRepo() (*repository.ExtractionRuleRepository, *sqlitedb.SQLiteClient, error) {
	dsn, err := dsninjector.Unmarshal(envDsnSqliteDB)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", envDsnSqliteDB, err)
	}

	db, err := sqlitedb.NewSQLiteClient(dsn, io.Discard)
	if err != nil {
		return nil, nil, fmt.Errorf("open sqlite: %w", err)
	}

	repo, err := repository.NewExtractionRuleRepository(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("build extraction rule repo: %w", err)
	}
	return repo, db, nil
}

func buildHTTPClient(proxyURL string, timeout time.Duration) *http.Client {
	transport := &http.Transport{}
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}
	return &http.Client{Timeout: timeout, Transport: transport}
}

func fetchURL(client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("build request: %w", err), internal.NewTraceError())
	}
	req.Header.Set("User-Agent", "RuleDoctor/1.0 (+https://github.com/seilbekskindirov/monitor)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("http get: %w", err), internal.NewTraceError())
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func bestRule(ex *ruledoctor.Extraction, vr ruledoctor.VerifyResult) (domain.Method, string) {
	if vr.CSSMatches && strings.TrimSpace(ex.CSSSelector) != "" {
		return domain.MethodCSS, ex.CSSSelector
	}
	if vr.RegexMatches && strings.TrimSpace(ex.Regex) != "" {
		return domain.MethodRegex, ex.Regex
	}
	if strings.TrimSpace(ex.CSSSelector) != "" {
		return domain.MethodCSS, ex.CSSSelector
	}
	return domain.MethodRegex, ex.Regex
}

func verifyFailReason(vr ruledoctor.VerifyResult) string {
	if !vr.ValueMatches {
		return "value mismatch"
	}
	reasons := []string{}
	if !vr.CSSMatches {
		msg := "css did not match"
		if vr.CSSError != nil {
			msg += ": " + vr.CSSError.Error()
		}
		reasons = append(reasons, msg)
	}
	if !vr.RegexMatches {
		msg := "regex did not match"
		if vr.RegexError != nil {
			msg += ": " + vr.RegexError.Error()
		}
		reasons = append(reasons, msg)
	}
	return strings.Join(reasons, "; ")
}

func writeHeader(w io.Writer, now time.Time, providerTag string, targetIDs []string) {
	_, _ = fmt.Fprintf(w, "-- ruledoctor run %s provider=%s\n", now.Format(time.RFC3339), providerTag)
	_, _ = fmt.Fprintf(w, "-- targets: %s\n", strings.Join(targetIDs, ", "))
	_, _ = fmt.Fprintf(w, "-- This file modifies extraction_rules ONLY. Schema is owned by Go migrations.\n")
	_, _ = fmt.Fprintf(w, "-- Re-applying this file is safe (unique active index prevents duplicates) but grows history.\n")
}

func emitSQL(w io.Writer, results []generateResult, now time.Time) {
	// Each (target, label) pair gets its own UPDATE+INSERT — the partial unique
	// index is keyed on (target_kind, target_id, label), so multiple labels for
	// the same target_id coexist. We must NOT dedup by target; emit once per pair.
	for _, r := range results {
		if r.skipped {
			_, _ = fmt.Fprintf(w, "\n-- %s label=%q: verification failed (%s). Skipped.\n", r.targetID, r.label, r.reason)
			continue
		}
		rule := r.rule

		_, _ = fmt.Fprintf(w, "\n-- %s label=%q: verified css=%v regex=%v value=%q\n",
			r.targetID, r.label, r.vr.CSSMatches, r.vr.RegexMatches, r.ex.Value)
		_, _ = fmt.Fprintf(w, "UPDATE extraction_rules\n")
		_, _ = fmt.Fprintf(w, "   SET status = 'superseded'\n")
		_, _ = fmt.Fprintf(w, " WHERE target_kind = %s AND target_id = %s AND label = %s AND status = 'active';\n",
			sqlQuote(string(rule.TargetKind)), sqlQuote(rule.TargetID), sqlQuote(rule.Label))

		var lastVerifiedSQL string
		if rule.LastVerifiedAt != nil {
			lastVerifiedSQL = fmt.Sprintf("%d", rule.LastVerifiedAt.Unix())
		} else {
			lastVerifiedSQL = fmt.Sprintf("%d", now.Unix())
		}

		_, _ = fmt.Fprintf(w, "INSERT INTO extraction_rules (id, target_kind, target_id, label, source_url,\n")
		_, _ = fmt.Fprintf(w, "    method, pattern, provider_tag, context_hash, status,\n")
		_, _ = fmt.Fprintf(w, "    generated_at, last_verified_at, notes)\n")
		_, _ = fmt.Fprintf(w, "VALUES (%s, %s, %s, %s, %s,\n",
			sqlQuote(rule.ID), sqlQuote(string(rule.TargetKind)), sqlQuote(rule.TargetID), sqlQuote(rule.Label), sqlQuote(rule.SourceURL))
		_, _ = fmt.Fprintf(w, "    %s, %s, %s, %s, %s,\n",
			sqlQuote(string(rule.Method)), sqlQuote(rule.Pattern), sqlQuote(rule.ProviderTag),
			sqlQuote(rule.ContextHash), sqlQuote(string(rule.Status)))
		_, _ = fmt.Fprintf(w, "    %d, %s,\n", rule.GeneratedAt.Unix(), lastVerifiedSQL)
		_, _ = fmt.Fprintf(w, "    %s);\n", sqlQuote(rule.Notes))
	}
}

func emitReport(w io.Writer, results []generateResult, providerTag string) {
	_, _ = fmt.Fprintf(w, "ruledoctor report  provider=%s\n\n", providerTag)
	_, _ = fmt.Fprintf(w, "%-30s  %-30s  %-8s  %-8s  %-8s  %s\n",
		"target_id", "label", "value", "css", "regex", "notes")
	for _, r := range results {
		if r.skipped {
			_, _ = fmt.Fprintf(w, "%-30s  %-30s  SKIPPED  (%s)\n", r.targetID, r.label, r.reason)
			continue
		}
		_, _ = fmt.Fprintf(w, "%-30s  %-30s  %-8v  %-8v  %-8v  %s\n",
			r.targetID, r.label,
			r.vr.ValueMatches, r.vr.CSSMatches, r.vr.RegexMatches, r.rule.Notes)
	}
}

func artifactPaths(outDir string, now time.Time) (sqlPath, reportPath string, err error) {
	date := now.Format("20060102")
	glob := filepath.Join(outDir, date+"-*-ruledoctor.sql")
	matches, _ := filepath.Glob(glob)
	seq := len(matches) + 1
	base := filepath.Join(outDir, fmt.Sprintf("%s-%s-%04d-ruledoctor",
		now.Format("20060102"), now.Format("150405"), seq))
	return base + ".sql", base + ".report.txt", nil
}

func generateRuleID(now time.Time) string {
	return fmt.Sprintf("R%04d%02d%02d%02d%02d%02dZ%dT%X",
		now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), now.Second(),
		now.Nanosecond(), uuid.NewV4().Bytes(),
	)
}

// sqlQuote wraps s in single quotes and doubles any embedded single quotes,
// producing a safe SQLite string literal. Never use fmt.Sprintf("'%s'", x).
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:8])
}

func resolveEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func resolveTimeout() time.Duration {
	v := os.Getenv(envTimeout)
	if v == "" {
		return defaultTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return defaultTimeout
	}
	return d
}

func splitTrim(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
