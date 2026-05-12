# Task Breakdown

## Overview

Stub plan — to be expanded by `gocode-architect` before implementation.

Configuration surfaces are documented inconsistently:

- `CLAUDE.md` lists `SQLITEDB_DSN` and `TELEGRAMBOT_DSN`.
- `.env.example` and `cmd/web/main.go` flags reference values (`--api-dsn`,
  logging knobs, possibly `HTTP_PORT`) that are not in CLAUDE.md.
- TODO comments remain in `internal/application/service/raterestapi.go` and
  `internal/gateway/httpV1/handlers/handlers.go` — either finish the work or remove
  the placeholders.
- `cmd/migrator` (see plan 003) will introduce another DSN consumer; documenting it
  in the same pass avoids drift.

Goal: a single authoritative table in `CLAUDE.md` (Environment Variables section)
listing every env var and CLI flag for every binary (`web`, `collector`, `notifier`,
`migrator`) with: name, type, required/optional, default, where it is consumed.
`.env.example` is then regenerated from that table so the two never disagree.

Open questions:

- Should we generate `.env.example` from a Go struct (single source of truth) or keep
  it hand-written? Hand-written is simpler now; a generator pays off only with a few
  more binaries.

## Assumptions

TBD.

## Tasks

TBD.

## Execution Order

TBD.

## Risks

TBD.

## Trade-offs

TBD.
