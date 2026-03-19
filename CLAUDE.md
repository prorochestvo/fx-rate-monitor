# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make build      # Build binary to ./build/monitor (CGO_ENABLED=0)
make test       # Run all tests with race detector
make lint       # Run go vet
make run ARGS="..." # Run with arguments
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