# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make build      # Build binary to ./build/monitor (CGO_ENABLED=0)
make test       # Run all tests with race detector
make lint       # Run go vet
make run        # Run collector app
make format     # Run go fmt
make clean      # Remove binary and database
```

Run a single test:
```bash
CGO_ENABLED=0 go test -race ./internal/forecaster/... -run TestMovingAverage
```

All `make` targets explicitly set `CGO_ENABLED=0` for portability.

## Architecture

The app periodically fetches FX rates, stores them in SQLite, and sends Telegram alerts when rates change significantly. It also provides short-term forecasts alongside notifications.

**Main flow** (`cmd/monitor/main.go`): load config → init storage → create extractor → create detector → create notifier → create forecaster → run scheduler.

**Core packages** (all under `internal/`):

| Package | Role |
|---------|------|
| `config` | CLI flags + JSON source config loading |
| `model` | Domain types: `Rate`, `ForecastResult` |
| `storage` | SQLite via `modernc.org/sqlite` (no CGO); runs embedded migrations on startup |
| `extractor` | HTTP fetch with optional proxy + regex-based rate extraction |
| `detector` | Dual-threshold change detection (absolute + percentage) |
| `notifier` | Telegram Bot API with retry logic |
| `forecaster` | `CompositeForecaster` averages results from `MovingAverage` + `LinearRegression` (uses `gonum`) |
| `scheduler` | Fixed-interval job runner with immediate first execution |

**Key design decisions:**
- All components are defined as interfaces, wired together in `main.go`
- SQL migrations are embedded via `//go:embed` in `storage.go` and run automatically
- CGO is disabled everywhere; pure Go SQLite (`modernc.org/sqlite`) avoids build toolchain requirements
- Cross-platform release binaries (linux/darwin, amd64/arm64) are published via the `prime.yml` workflow on `v*.*.*` tags

## Configuration

The binary requires a JSON sources config (`--config`). See `configs/sources.example.json` for format — each source defines a URL and regex rules to extract rates from HTML.

Key CLI flags: `--db`, `--config`, `--interval`, `--telegram-token`, `--telegram-chat`, `--abs-threshold`, `--pct-threshold`, `--proxy`, `--log-format`.

## Testing Conventions

All tests in this project MUST follow these rules. No exceptions.

### 1. Use subtests (`t.Run`) for organisation

Every test function groups its cases with `t.Run`. Naming pattern: `TestType_Method`.

```go
func TestMyType_Method(t *testing.T) {
    t.Parallel()

    t.Run("returns result on valid input", func(t *testing.T) {
        t.Parallel()
        // ...
    })
    t.Run("returns error on invalid input", func(t *testing.T) {
        t.Parallel()
        // ...
    })
}
```

### 2. File layout order (per test file)

Top → bottom:

1. Test-case tables / input structs
2. Test functions (with subtests)
3. Helper functions
4. Constants (if any)
5. Mock / test-double structs

No blank lines between subtests. No section-separator comments.

### 3. Positive cases before negative cases

Within every `t.Run` group, the **first** subtest MUST be a happy-path / success case.
Error-handling and edge-case subtests follow. Every test function must have **at least one**
positive case.

### 4. Always call `t.Parallel()`

Both the outer test function and every `t.Run` subtest must call `t.Parallel()` as their
first statement (before any setup code).

### 5. Use `testify` for assertions

Use `require` for fatal assertions (test stops on failure) and `assert` for non-fatal ones.
Prefer `require.NoError`, `require.Equal`, `require.ErrorIs`, etc.  
Do **not** use bare `if err != nil { t.Fatal(...) }` patterns.

### 6. `TestMain` / `main_test.go` only for global shared resources

Create `main_test.go` (with `TestMain`) only when a package-level shared resource is needed
(e.g. a Docker container, a single shared DB connection). Otherwise organise tests in files
that mirror the file under test: `storage.go` → `storage_test.go`.

### 7. Use `t.Context()` for context arguments

If the function under test accepts a `context.Context`, pass `t.Context()` in tests — do not
use `context.Background()` or `context.TODO()`.

```go
result, err := svc.Fetch(t.Context(), id)
require.NoError(t, err)
```

### 8. Regression test for every bug and new edge case

Whenever a bug is discovered **or** an edge case is identified that was not previously
covered, a dedicated regression test **MUST** be added before the fix is merged.

**Rules:**
- The test must reproduce the bug / edge case as a failing test *before* the fix is applied.
- The test must pass *after* the fix.
- Name the subtest descriptively so the failure message is self-explanatory:

```go
t.Run("does not panic on empty rate slice", func(t *testing.T) {
    t.Parallel()
    require.NotPanics(t, func() { _ = forecaster.Forecast([]model.Rate{}) })
})
```

- If the bug spans multiple packages, add a regression test in **each** affected package.
- Reference the issue / PR number in a comment above the test (if applicable):

```go
// Regression: github.com/you/fx_rate_monitor/issues/42
t.Run("returns ErrNoRates when history is empty", func(t *testing.T) { ... })
```

**Rationale:** Regression tests act as a living specification of known failure modes.
They prevent silent re-introduction of the same bug in future refactors.

## Mandatory Checks After Every Change

After **any** code or configuration change — no matter how small — you MUST run both
commands and confirm they pass before considering the task done:

```bash
make build   # must exit with code 0
make test    # must exit with code 0
```

A change that breaks the build or any test is **not complete**, regardless of whether the
logic looks correct.  Fix the breakage before moving on.