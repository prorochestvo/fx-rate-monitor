# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Template note.** This is a project-agnostic starter. Replace the `<...>` placeholders
> and the example sections below with the real values for your project. Anything inside
> a `<...>` is a marker that must be filled in or removed before the file is considered
> complete.

## Build & Run Commands

All commands assume a pure-Go build with `CGO_ENABLED=0` (recommended default). Adjust
if your project legitimately needs CGO.

```bash
make build    # Builds binaries to ./build/
make run      # Runs the application (migrations + service start, if applicable)
make test     # go fmt + go vet + go test -race ./...
make lint     # go vet + checks for forbidden imports
make format   # go fmt ./...
make clean    # Removes binaries + go mod tidy
```

Targeted test runs:
```bash
# Single top-level test
CGO_ENABLED=0 go test -race -run TestFunctionName ./<package>/

# Single subtest
CGO_ENABLED=0 go test -race -run 'TestFunctionName/subtest_name' ./<package>/

# Verbose output (see every subtest pass/fail)
CGO_ENABLED=0 go test -race -v ./<package>/

# Benchmarks
CGO_ENABLED=0 go test -bench=. -benchmem -run=^$ ./<package>/

# Coverage
CGO_ENABLED=0 go test -race -coverprofile=cover.out ./... && go tool cover -html=cover.out
```

## Architecture Overview

<One-paragraph description of what this service does and its main flow.>

### Layer Responsibilities

Replace the rows below with the real layout of your project. The example below uses a
common layered Go layout — keep, edit, or remove rows as needed.

| Layer | Location | Role |
|-------|----------|------|
| Entry point | `cmd/<binary>/` | Composition root, server bootstrap |
| Application | `internal/application/service/` | Business logic orchestration |
| Domain | `internal/domain/` | Value objects / models, no logic |
| Gateway | `internal/gateway/` | Routers, controllers, middleware |
| Repository | `internal/repository/` | Persistence queries |
| Infrastructure | `internal/infrastructure/` | External clients (DB, third-party APIs) |
| Tools | `internal/tools/` | Cross-cutting utilities |

### Key Patterns

- **Repository pattern** — each repository type owns its own SQL, migration, and query helper functions. Queries execute inside explicit transactions (`r.db.Transaction(ctx)`). Repositories are passed as interfaces into service and handler layers.
- **Configuration injection** — `SQLITEDB_DSN` and `TELEGRAMBOT_DSN` are read via `dsninjector.Unmarshal(envName)` at startup in `cmd/web/main.go` and live in the systemd `EnvironmentFile`. The public HTTPS origin is passed via the `--api-dsn` CLI flag (format: `https://<host>/`, parsed by `dsninjector.Parse`) and is hardcoded in the systemd unit's `ExecStart` line — never in `.env`. All three configs must be present at startup; the binary calls `log.Fatalf` on any missing value.
- **Embedded assets** — `cmd/web/main.go` embeds the `static/` directory via `//go:embed static`. All static files served by `http.FileServer` live under `cmd/web/static/`.
- **Auth: Telegram WebApp initData HMAC** — the `/api/me/...` endpoint family authenticates callers by verifying the Telegram WebApp `initData` HMAC-SHA256 signature. The signing algorithm uses `secret_key = HMAC_SHA256("WebAppData", botToken)` (the string literal is the key; the token is the message). Implementation lives in `internal/tools/tgwebapp/initdata.go`. The handler injects the validator as a function field so tests can substitute a fake without real bot tokens.

### HTTP Routes

- `GET /api/sources` — list all configured rate sources with latest execution status
- `GET /api/sources/{name}/rates` — most recent rate values for a named source
- `GET /api/sources/{name}/rates/chart` — aggregated chart data (period=week|month|year)
- `GET /api/sources/{name}/history` — execution history for a named source
- `GET /api/sources/{name}/events/failed` — paginated failed events for a source
- `GET /api/sources/{name}/subscriptions` — grouped subscription statistics for a source
- `GET /api/sources/{name}/subscriptions/list` — paginated subscription details for a source
- `GET /api/sources/{name}/events/daily` — daily aggregated event counts for a source
- `PATCH /api/sources/{name}/active` — enable or disable a named source
- `GET /api/stats` — global statistics (source counts, error count)
- `GET /api/errors/execution` — paginated failed execution history records
- `GET /api/events/pending` — all currently pending notification events
- `GET /api/notifications` — last N notification pool records
- `GET /api/notifications/failed` — all failed notification pool records
- `GET /api/me/subscriptions` — caller's own subscriptions enriched with latest rate values; authenticated via Telegram WebApp initData HMAC (`X-Telegram-Init-Data` header or `?initData=` query string fallback)
- `GET /app/subscriptions.html` — Telegram Mini App HTML page (served by embedded static file server; no dedicated route needed)

### Database

Describe the database (engine, driver, migration tooling) and list the migration files
or tables that exist. Example shape:

| Migration | Table(s) |
|-----------|----------|
| `001_<name>.sql` | `<table>` |
| ... | ... |

### Environment Variables

- `SQLITEDB_DSN` — SQLite connection string, parsed via `dsninjector.Unmarshal`. Format: `sqlite://<path-to-db-file>`
- `TELEGRAMBOT_DSN` — Telegram bot credentials parsed via `dsninjector.Unmarshal`. Format: `<adminChatID>:<botToken>@<host>` where `Addr()` returns the token and `Login()` returns the admin chat ID.

> The public HTTPS origin of the `cmd/web` server is **not** an env var — see the `--api-dsn` CLI flag on the `cmd/web` binary, baked into the systemd unit's `ExecStart` line.

> Never read or edit `.env` files.

### Key Dependencies

List third-party libraries the project depends on and the Go version. Example shape:

- `<module path>` — purpose
- ...
- Go version: `<x.y.z>`

### Frontend (if any)

Describe any embedded or served frontend assets and their location.

### Deployment

Describe how the service is deployed (systemd unit, Docker, k8s, etc.).

## Error Handling

Define the project's error-handling contract here. The example below describes a
common pattern of separating user-facing errors from internal failures — keep, adapt,
or replace it.

### `PublicError` — user-facing errors (example pattern)

A dedicated error type (commonly `internal.PublicError` or similar) is the mechanism for
surfacing safe, human-readable messages to end users. Any error message that is **safe
to show** to a user is wrapped with `internal.NewPublicError(...)` at the point where
the error is created (typically in the service layer).

**Rule**: if a function can fail in a way that meaningfully communicates something to
the user, return a public error. For all other failures (DB down, unexpected nil, etc.)
return a plain error — the controller will send a generic fallback.

#### Creating a public error (service layer)

```go
import "<module>/internal"

// User should know about this
return internal.NewPublicError("Invalid input. <specific guidance>")

// Internal failure — user gets generic message
return fmt.Errorf("db query failed: %w", err)
```

#### Error handling in the controller

The controller catches all errors from sub-handlers and sends the appropriate message.

```go
const errFallbackMessage = "Something went wrong. Try again later."
```

| Situation | What service returns | What user sees |
|-----------|---------------------|----------------|
| Expected business failure (validation, state) | `internal.NewPublicError("...")` | The exact message from `PublicError.Details()` |
| Unexpected / infrastructure failure | plain `error` | The fallback message |
| No error | `nil` | Normal happy-path response |

### Testing the error path

Every controller test that exercises an error branch **must** assert:

1. That a response was actually sent (the user is not left in silence).
2. That the sent text equals `PublicError.Details()` when the error is a `PublicError`.
3. That the sent text equals the fallback constant when the error is a plain error.

## Constraints

Replace this section with the real constraints of your project. The list below is a
reasonable default for a CGO-free Go service — keep what applies, drop what doesn't,
add what's missing.

- **Forbidden imports**: list any modules that must never appear in `go.mod` (e.g.
  CGO-dependent SQLite drivers, code generators the team has rejected). Enforce via
  `make lint`.
- **Testing**: Use `github.com/stretchr/testify`; run tests with `-race`; parallel
  subtests preferred where there's no shared mutable state.
- **One `Test*` per method, scenarios as subtests**: each tested method/function gets
  exactly one top-level test function named after it (e.g. `TestEncode` for `Encode`),
  and every scenario for that method lives as a `t.Run("descriptive name", ...)`
  subtest inside it. Do **not** create separate top-level tests like
  `TestEncode_EmptyInput`, `TestEncode_Unicode`, `TestEncode_Error` — these belong
  as subtests of a single `TestEncode`. Methods on a type follow the same rule with
  the standard `TestType_Method` form (e.g. `TestUser_Validate`).
  ```go
  func TestEncode(t *testing.T) {
      t.Parallel()

      t.Run("empty input returns empty string", func(t *testing.T) {
          t.Parallel()
          // ...
      })

      t.Run("unicode is preserved", func(t *testing.T) {
          t.Parallel()
          // ...
      })

      t.Run("returns error on invalid byte", func(t *testing.T) {
          t.Parallel()
          // ...
      })
  }
  ```
- **No CGO**: `CGO_ENABLED=0` must be set for all build and test commands (unless the
  project intentionally requires CGO).
- **Compile-time interface checks**: Every mock/stub struct in test files must have a
  `var _ interfaceName = &mockStruct{}` assertion at the top of the file.
- **No section-divider comments**: Do not use `// --- section ---` or `// ----` style
  separator comments. Let the code structure speak for itself.
- **No skipped errors**: Never use `_` to discard error return values in production or
  test code. Always capture the error and assert/check it. The only exceptions are
  `fmt.Fprint*` writes to loggers, `Rollback()` calls in error-recovery paths, and
  resource `.Close()` in `t.Cleanup` / `defer`.
- **UI conventions**: Document any project-specific rules about emojis, copy, or
  formatting in user-facing surfaces.

## Planning Workflow

All non-trivial work is tracked as a Markdown plan file before implementation begins.

### Directory layout

```
plans/
├── NNN-task-slug.md     # active / in-progress plans (e.g. 001-fix-auth.md)
├── completed/           # plans for fully shipped tasks (e.g. 260422.0001.fix-auth.md)
└── history/             # archived / cancelled plans
```

### File naming

- **Active plans (`plans/`)** — zero-padded sequential index + kebab-case slug:
  `NNN-description.md` (e.g. `001-fix-unauthorized-middleware.md`, `002-add-rate-limiting.md`).
  Pick the next number by checking the highest existing prefix across `plans/`, `plans/completed/`,
  and `plans/history/`.

- **Completed plans (`plans/completed/`)** — date prefix + zero-padded daily index (4 digits) + slug:
  `YYMMDD.NNNN.description.md` (e.g. `260422.0001.fix-unauthorized-middleware.md`).
  `NNNN` resets to `0001` each day and increments for each additional completion on that day.

- **Archived plans (`plans/history/`)** — keep the original `NNN-` filename from `plans/`.

### Lifecycle

1. **Create** — before touching code, produce a plan file in `plans/` using the `NNN-slug.md`
   naming convention described above.
2. **Implement** — work through the tasks defined in the plan. The plan file stays in
   `plans/` while work is in progress.
3. **Complete** — once every acceptance criterion is met and `make test` passes, rename
   and move the file to `plans/completed/` using the date-based convention:
   ```bash
   mv plans/001-fix-auth.md plans/completed/260422.0001.fix-auth.md
   ```
4. **Archive** — if a plan is abandoned or superseded without being fully implemented or
   if we need to save intermediate data or task execution logs, move it to `plans/history/` instead.

### Plan file format

Every plan file follows this structure:

```markdown
# Task Breakdown

## Overview
## Assumptions
## Tasks
### Task N: <Title>
- Description:
- Acceptance Criteria:
- Pitfalls & edge cases:
- Complexity: Easy / Medium / Hard
## Execution Order
## Risks
## Trade-offs
```

### Rules

- **One plan per concern.** Don't bundle unrelated changes in a single plan file.
- **Plan before code.** Claude must create (or confirm an existing) plan file before
  writing or modifying any source files.
- **Keep plans honest.** If implementation diverges from the plan, update the plan file
  before moving it to `completed/`.
- **Slug matches intent.** The description part of the filename should be readable at a glance:
  `002-add-rate-limiting.md`, `003-migrate-sqlite-to-postgres.md`, not `004-task.md`.

## Agent Pipeline

All non-trivial tasks follow a three-stage pipeline using specialized agents. A fourth
agent (`gocode-testdoctor`) is invoked on-demand whenever tests fail, at any stage.

```
User describes task
    ↓
1. gocode-architect
    → Creates plan file at plans/NNN-slug.md (see Planning Workflow)
    ↓
2. gocode-engineer
    → Implements the tasks defined in the plan
    ↓
3. gocode-reviewer
    → Reviews the implementation
    ↓
  ❌ Problems?       → Back to gocode-engineer with specific findings
  ⚠️  Tests failing? → gocode-testdoctor diagnoses and patches, then rerun reviewer
  ✅ Approved?       → mv plans/NNN-slug.md plans/completed/YYMMDD.NNNN.slug.md
```

### Agent responsibilities

| Agent | Owns | Output |
|-------|------|--------|
| `gocode-architect` | Planning, decomposition, trade-offs | New plan file in `plans/` |
| `gocode-engineer` | Implementation, tests for new code | Code + tests in the repo |
| `gocode-reviewer` | Verdicts, severity-ranked findings, patches | Review report; moves plan to `completed/` on approval |
| `gocode-testdoctor` | Triage of failing tests, minimal patches | Code/test fixes, re-run of `make test` |

### Rules

- **No skipping stages.** Every task starts with the architect and ends with reviewer approval.
- **Plan file first.** The architect MUST produce a plan file before any code is written. If a plan already exists for the task, update it rather than creating a new one.
- **Review loop.** Engineer ↔ Reviewer cycle repeats until the reviewer approves.
- **Reviewer gates completion.** Only the reviewer moves the plan to `plans/completed/` with the `YYMMDD.NNNN.slug.md` rename.
- **`make test` must pass** before the reviewer gives approval. If it fails, hand the logs to `gocode-testdoctor`.
- **Testdoctor is scoped.** It patches tests or the minimal production code needed to make the failure go away. It does not redesign or refactor.
