// Command rulegen generates an extraction rule for a named rate source by asking
// an LLM, validating the rule against the live source URL, and persisting the
// result to the SQLite database.
//
// Usage:
//
//	rulegen <source-name> [flags]
//	rulegen --all [flags]
//
// Single-source mode requires exactly one positional argument (the source name).
// --all mode iterates every active row in rate_sources; it is mutually exclusive
// with a positional source-name argument.
//
// Flags:
//
//	--all                      iterate every active source (cron mode)
//	--force-fallback           skip primary, go straight to fallback
//	--max-primary-attempts N   max primary attempts before escalation (default 3)
//	--max-fallback-attempts N  max fallback attempts before total failure (default 2)
//	--logs-dir DIR             path to logs directory
//	--verbosity LEVEL          minimum log level (debug|info|warning|error|severe|critical)
//
// Exit codes (single-source mode):
//
//	0  success — rule generated and persisted
//	1  generation failed — source exists but no valid rule could be produced
//	2  usage error — missing argument, malformed flag, or --all combined with positional arg
//	3  infrastructure error — DB unreachable or migrations not applied
//
// Exit codes (--all mode):
//
//	0  normal completion — per-source failures are logged and counted but never escalated
//	   as a non-zero exit. This mirrors the resilience pattern used by cmd/collector and
//	   cmd/notifier (commit 3229715) so that cron does not page on transient LLM hiccups.
//	   Check the summary line in stdout for succeeded/failed/skipped counts.
//	3  infrastructure error — DB unreachable, migrations not applied, or logger/AI client
//	   init failure. Identical semantics to single-source mode exit code 3.
//
// Environment variables:
//
//	SQLITEDB_DSN      (required) SQLite connection string
//	AI_PRIMARY_DSN    (required) primary AI provider DSN
//	AI_FALLBACK_DSN   (optional) fallback AI provider DSN; stub used when absent
//	CHROMIUM_PATH     (optional) absolute path to Chromium/Chrome binary;
//	                  defaults to chromedp PATH lookup (chromium, chromium-browser, google-chrome, chrome)
//
// Each invocation makes up to maxPrimaryAttempts + maxFallbackAttempts LLM calls per
// source. With many active sources, --all can make dozens of LLM calls in a single
// run. Ensure your provider account has sufficient budget, and set a generous cron
// timeout (at least 30 minutes for a typical deployment with ~10 active sources).
//
// When using a stub fallback (no AI_FALLBACK_DSN), metadata.Model will record "stub".
// This is intentional — it makes stub-generated rules trivially greppable in the DB.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"time"

	"github.com/prorochestvo/dsninjector"
	"github.com/seilbekskindirov/monitor/internal"
	"github.com/seilbekskindirov/monitor/internal/application/rulegen"
	"github.com/seilbekskindirov/monitor/internal/application/sourceaudit"
	"github.com/seilbekskindirov/monitor/internal/domain"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/artificialintelligence"
	"github.com/seilbekskindirov/monitor/internal/infrastructure/sqlitedb"
	"github.com/seilbekskindirov/monitor/internal/repository"
	_ "modernc.org/sqlite"
)

var (
	// BuildVersion is the application version string, injected at link time via -ldflags.
	BuildVersion = "dev"
	// BuildTime is the build timestamp, injected at link time via -ldflags.
	BuildTime = "unknown"
	// BuildHash is the VCS commit hash, injected at link time via -ldflags.
	BuildHash = "undefined"
	// LogsDir is the directory where log files are written.
	LogsDir = path.Join(os.TempDir(), "logs")
	// LogVerbosity controls the minimum log level emitted by the logger.
	LogVerbosity = internal.LogLevelWarning
)

const (
	envDsnSqliteDB   = "SQLITEDB_DSN"
	envDsnAIPrimary  = "AI_PRIMARY_DSN"
	envDsnAIFallback = "AI_FALLBACK_DSN"
	// envChromiumPath is an optional absolute path to the Chromium/Chrome binary.
	// When unset, chromedp falls back to its own PATH lookup order:
	// chromium, chromium-browser, google-chrome, chrome.
	envChromiumPath = "CHROMIUM_PATH"
)

func main() {
	os.Exit(run())
}

// rateSourceLister is the narrow read-side surface cmd/rulegen needs.
// Defined locally so tests can fake it without depending on the concrete repository.
type rateSourceLister interface {
	ObtainAllRateSources(ctx context.Context) ([]domain.RateSource, error)
}

// ruleGenerator is the narrow generate surface cmd/rulegen needs.
// Defined locally so tests can fake it without rebuilding the full dependency graph.
type ruleGenerator interface {
	Generate(ctx context.Context, sourceName string, forceFallback bool) (*rulegen.Result, error)
}

// runAll iterates every active rate source and invokes gen.Generate for each.
// It fetches all rate sources (both active and inactive) via ObtainAllRateSources
// and counts inactive rows as skipped, filtering in Go rather than in SQL so the
// summary line always reflects the full source inventory (plan trade-off R2).
// Per-source failures are logged to out and counted but never propagated; the return
// value is always 0 so cron does not page on partial failure. Panics inside a
// per-source call are recovered, logged, and counted as failures. Infrastructure
// errors (lister failure) are written to errOut and still return 0 with a zero-count
// summary line on out so that "grep rulegen --all: cron.log" always matches.
func runAll(ctx context.Context, gen ruleGenerator, srcs rateSourceLister, forceFallback bool, out, errOut io.Writer) int {
	sources, err := srcs.ObtainAllRateSources(ctx)
	if err != nil {
		fmt.Fprintf(errOut, "FAIL mode=--all reason=list sources: %v\n", err)
		fmt.Fprintf(out, "rulegen --all: processed=0 succeeded=0 failed=0 skipped=0\n")
		return 0
	}

	var processed, succeeded, failed, skipped int
	for _, src := range sources {
		if !src.Active {
			skipped++
			fmt.Fprintf(out, "SKIP source=%s reason=inactive\n", src.Name)
			continue
		}
		processed++
		func() {
			defer func() {
				if r := recover(); r != nil {
					failed++
					fmt.Fprintf(out, "FAIL source=%s reason=panic: %v\n", src.Name, r)
				}
			}()
			res, gerr := gen.Generate(ctx, src.Name, forceFallback)
			if gerr != nil {
				failed++
				fmt.Fprintf(out, "FAIL source=%s reason=%v\n", src.Name, gerr)
				return
			}
			succeeded++
			fmt.Fprintf(out, "OK source=%s rules=%d value=%g attempts=%d\n",
				src.Name, len(res.Rules), res.Value, res.AttemptsUsed)
		}()
	}
	fmt.Fprintf(out, "rulegen --all: processed=%d succeeded=%d failed=%d skipped=%d\n",
		processed, succeeded, failed, skipped)
	return 0
}

func run() int {
	allSources := flag.Bool("all", false, "iterate every active source (cron mode; always exits 0)")
	forceFallback := flag.Bool("force-fallback", false, "skip primary, go straight to fallback")
	maxPrimary := flag.Int("max-primary-attempts", 3, "max primary attempts before escalation")
	maxFallback := flag.Int("max-fallback-attempts", 2, "max fallback attempts before total failure")
	logsDir := flag.String("logs-dir", LogsDir, "path to logs directory")
	verbosity := flag.String("verbosity", "warning", "minimum stdout log level (debug, info, warning, error, severe, critical)")
	flag.Parse()

	args := flag.Args()

	if *allSources && len(args) > 0 {
		fmt.Fprintln(os.Stderr, "usage error: --all and a positional source name are mutually exclusive")
		fmt.Fprintln(os.Stderr, "")
		flag.PrintDefaults()
		return 2
	}

	if !*allSources && len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: rulegen <source-name> [flags]")
		fmt.Fprintln(os.Stderr, "       rulegen --all [flags]")
		fmt.Fprintln(os.Stderr, "")
		flag.PrintDefaults()
		return 2
	}

	if dir := *logsDir; dir != "" {
		LogsDir = dir
	}
	if v := *verbosity; v != "" {
		LogVerbosity = internal.ParseLogLevel(v)
	}

	// infraFail prints an infrastructure FAIL line to stderr.
	// In --all mode it uses "mode=--all" so the key is not mistaken for a source name.
	// In single-source mode it uses "source=<name>".
	var infraFail func(format string, args ...any)
	if *allSources {
		infraFail = func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "FAIL mode=--all reason="+format+"\n", args...)
		}
	} else {
		sourceName := args[0]
		infraFail = func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, "FAIL source="+sourceName+" reason="+format+"\n", args...)
		}
	}

	l, err := internal.NewLogger(LogsDir, "rulegen", LogVerbosity)
	if err != nil {
		infraFail("logger init: %v", err)
		return 3
	}

	dsnSQLiteDB, err := dsninjector.Unmarshal(envDsnSqliteDB)
	if err != nil {
		infraFail("settings %s: %v", envDsnSqliteDB, err)
		return 3
	}

	dsnAIPrimary, err := dsninjector.Unmarshal(envDsnAIPrimary)
	if err != nil {
		infraFail("settings %s: %v", envDsnAIPrimary, err)
		return 3
	}

	db, err := sqlitedb.NewSQLiteClient(dsnSQLiteDB, l.WriterAs(internal.LogLevelInfo))
	if err != nil {
		infraFail("sqlite connection: %v", err)
		return 3
	}
	defer func() {
		if e := db.Close(); e != nil {
			log.Printf("close sqlite: %v", e)
		}
	}()

	if err = sqlitedb.RequireMigratedSchema(context.Background(), db); err != nil {
		infraFail("schema check: %v", err)
		return 3
	}

	aiPrimary, err := artificialintelligence.NewClient(dsnAIPrimary, l.WriterAs(internal.LogLevelInfo))
	if err != nil {
		infraFail("ai primary client: %v", err)
		return 3
	}

	var aiFallback artificialintelligence.AIClient
	if _, ok := os.LookupEnv(envDsnAIFallback); ok {
		dsnAIFallback, dsnErr := dsninjector.Unmarshal(envDsnAIFallback)
		if dsnErr != nil {
			infraFail("settings %s: %v", envDsnAIFallback, dsnErr)
			return 3
		}
		aiFallback, err = artificialintelligence.NewClient(dsnAIFallback, l.WriterAs(internal.LogLevelInfo))
		if err != nil {
			infraFail("ai fallback client: %v", err)
			return 3
		}
	} else {
		aiFallback, err = artificialintelligence.NewStubClient()
		if err != nil {
			infraFail("ai fallback stub: %v", err)
			return 3
		}
	}

	rRateSource, err := repository.NewRateSourceRepository(db)
	if err != nil {
		infraFail("rate source repo: %v", err)
		return 3
	}

	plainFetcher := &sourceAuditFetcherAdapter{inner: sourceaudit.NewHTTPFetcher(time.Minute)}

	chromedpFor := func(waitSelector string) rulegen.Fetcher {
		return rulegen.NewChromedpFetcher(rulegen.ChromedpFetcherOptions{
			ChromiumPath: os.Getenv(envChromiumPath),
			Logger:       l.WriterAs(internal.LogLevelInfo),
			WaitSelector: waitSelector,
		})
	}

	gen, err := rulegen.NewGenerator(
		aiPrimary,
		aiFallback,
		plainFetcher,
		chromedpFor,
		rulegen.NewRuleExecutor(),
		rRateSource,
		*maxPrimary,
		*maxFallback,
		l.WriterAs(internal.LogLevelInfo),
	)
	if err != nil {
		infraFail("build generator: %v", err)
		return 3
	}

	if *allSources {
		return runAll(context.Background(), gen, rRateSource, *forceFallback, os.Stdout, os.Stderr)
	}

	sourceName := args[0]
	res, err := gen.Generate(context.Background(), sourceName, *forceFallback)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL source=%s reason=%v\n", sourceName, err)
		if errors.Is(err, rulegen.ErrUnsupportedFetcherKind) {
			return 2
		}
		return 1
	}

	fmt.Printf("OK source=%s rules=%d value=%g attempts=%d escalated=%t provider=%s model=%s\n",
		sourceName,
		len(res.Rules),
		res.Value,
		res.AttemptsUsed,
		res.Escalated,
		res.Metadata.Provider,
		res.Metadata.Model,
	)
	return 0
}

// sourceAuditFetcherAdapter wraps sourceaudit.Fetcher to satisfy the
// rulegen.Fetcher interface, which returns only the body bytes.
type sourceAuditFetcherAdapter struct {
	inner sourceaudit.Fetcher
}

func (a *sourceAuditFetcherAdapter) Fetch(ctx context.Context, url string) ([]byte, error) {
	result, err := a.inner.Fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	return result.Body, nil
}
