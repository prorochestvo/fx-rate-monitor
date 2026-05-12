# Task Breakdown

## Overview

Stub plan — to be expanded by `gocode-architect` before implementation.

The service exposes no `/health` or `/metrics` endpoints. Without them:

- Systemd / load balancers / future k8s deployments cannot probe liveness or readiness.
- There is no Prometheus surface; we cannot see request counts, latencies, error
  rates, DB transaction durations, or notification queue depth.

We need at minimum:

- `GET /health` — returns 200 once the DB is reachable (cheap `SELECT 1` ping with a
  short timeout); 503 otherwise. JSON body with `status`, `db`, and `version` fields.
- `GET /metrics` — Prometheus exposition format. Counters for HTTP requests by
  route/status, histogram for handler latency, a gauge for pending notification queue
  size, and DB query duration histogram.

Open questions to resolve in the full plan:

- Library choice: `prometheus/client_golang` vs hand-rolled.
- Auth: should `/metrics` be on the public listener or a separate admin port?
- How to wire counters through the existing gateway middleware chain without leaking
  metrics into the domain layer.

## Assumptions

TBD.

## Tasks

TBD — to be filled in.

## Execution Order

TBD.

## Risks

TBD.

## Trade-offs

TBD.
