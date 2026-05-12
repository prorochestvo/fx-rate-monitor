# Task Breakdown

## Overview

Stub plan — to be expanded by `gocode-architect` before implementation.

Handlers in `internal/gateway/httpV1/handlers/handlers.go` parse query parameters
(`limit`, `offset`, `period`, `sourceName`, …) without validation. Examples:

- `limit` / `offset` accept negative numbers and are passed straight to repositories.
- `sourceName` is taken from the URL path with no character whitelist and embedded into
  SQL via the repository helpers (parameterised today, but no defensive validation
  layer).
- `period` for chart endpoints accepts arbitrary strings; only the success path is
  exercised by tests.

We need a small validation layer (middleware or per-handler helpers) that:

- Clamps / rejects negative `limit` / `offset`, caps `limit` at a sane upper bound.
- Whitelists `period` values (`week|month|year`).
- Restricts `sourceName` to `[a-zA-Z0-9_-]{1,64}`.
- Returns a `PublicError` with a clear message instead of letting bad input reach the
  repository.

Open questions:

- Middleware vs explicit per-handler validation. Middleware risks coupling to handler
  signatures; explicit calls are more verbose but obvious.
- Where to put validation primitives (`internal/tools/validate/` ?).

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
