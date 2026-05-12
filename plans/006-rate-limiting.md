# Task Breakdown

## Overview

Stub plan — to be expanded by `gocode-architect` before implementation.

The HTTP API has no rate limiting. `/api/*` and especially `/api/me/*` (authenticated
via Telegram WebApp HMAC) are open to unlimited request volume from a single client.
Risks:

- A misbehaving Mini App or compromised Telegram client can hammer
  `/api/me/subscriptions` and exhaust DB connections.
- Bot scanners hitting `/api/sources` repeatedly cost CPU and burn through DB I/O.
- We have no defence against accidental loops in the WASM frontend.

Target shape:

- Per-IP global limiter on `/api/*` (e.g. 100 req/sec sustained, burst 200).
- Per-Telegram-user limiter on `/api/me/*` (e.g. 10 req/sec, burst 30) keyed by the
  authenticated chat ID extracted from `initData`.
- Standard `429 Too Many Requests` + `Retry-After` response when the limit is hit.

Open questions:

- In-memory `golang.org/x/time/rate` is enough for single-instance deploy; revisit if
  we ever scale horizontally.
- Should `/metrics` and `/health` be exempt? (Yes — assume so.)

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
