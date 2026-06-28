# Task Breakdown

## Overview

Rename the project from `monitor` / `fx_rate_monitor` to **beacon** across code,
active configuration, CI, and docs, and migrate the public deployment from the
`be-happy.kz` / `stage.be-happy.kz` Let's Encrypt dual-env setup to a single
Cloudflare-fronted production domain `https://beacon.seilbekskindirov.dev/`.

**Stage is dropped.** For this project a permanent stage earns nothing: it would
scrape the same live bank pages, talk to the same Telegram, and its only real
value (not breaking prod data / not spamming subscribers) is cheaper to get
on-demand — local `go run` against a pulled prod snapshot (`make backups`) plus
a throwaway test bot behind a tunnel when a bot/Mini-App change needs eyes.

The edge and CI model mirror the `vpntunnel` repo: Cloudflare-proxied DNS, a
shared `*.seilbekskindirov.dev` Cloudflare **Origin** certificate, Authenticated
Origin Pulls (mTLS), and a two-workflow CI — `ci.main.yml` (lint+test on every
push/PR to `main`) and `release.yml` (deploy on an `r_*` release tag). The certbot /
balancer / shadow nginx split is dropped.

The same physical host (`be-happy.kz` SSH alias) is reused. Live production data
is migrated from `/opt/monitor` to `/opt/beacon` (server-side steps documented
in the runbook below, not automated here).

## Assumptions

- Go module path becomes `github.com/seilbekskindirov/beacon` (repo already
  renamed on GitHub by the operator).
- Flat single-prod naming (vpntunnel-style): `/opt/beacon`, binaries
  `webapp`/`collector`/`notifier`/`migrator` (bare, the dir namespaces them), systemd unit `beacon`, env file
  `/opt/beacon/.env`, DB `/opt/beacon/beacon.sqlite`.
- Historical artifacts are **not** rewritten: `plans/completed/`,
  `plans/history/`, active plan files `012`/`013`/`HANDOFF`, and `docs/reviews/`
  describe past state and stay as-is.
- The English word "monitor"/"monitoring" used descriptively in godoc stays.
- External dependency import paths `github.com/prorochestvo/*` stay (real
  third-party modules); only the systemd `Description` brand label changes.
- App still serves plain HTTP on loopback (`--port 8000`); nginx terminates TLS
  and proxies `http://127.0.0.1:8000`.
- The content-hashed static-asset edge cache and gzip behaviour are preserved
  (they are real features of this app, documented in CLAUDE.md).

## Tasks

### Task 1: Go module path rename
- Description: `go.mod` module line + every `github.com/seilbekskindirov/monitor`
  import in `*.go` (including `_test.go`) → `.../beacon`. `go mod tidy`.
- Acceptance: `go build ./...` and `go vet ./...` clean; no remaining
  `seilbekskindirov/monitor` in any `*.go` or `go.mod`.
- Complexity: Easy (mechanical sed, scoped to `*.go`).

### Task 2: Product branding in code/UI
- Description: rename user-facing brand "FX Rate Monitor" / `fx-rate-monitor`
  (HTML `<title>`, `<h1>`, app-loader, console tag, godoc "Command …" lines in
  `cmd/doctor/main.go` and `cmd/wasm/main.go`) → "Beacon". KEEP the notification
  body title "FX rates" and the descriptive English-word comments.
- Acceptance: UI shows Beacon; `make test` still green (notification tests
  untouched).
- Complexity: Easy.

### Task 3: systemd unit
- Description: replace the per-env `srv.{prime,stage}_monitor.service` with a
  single flat `configs/beacon.service`: `Description=Beacon web service`,
  `WorkingDirectory=/opt/beacon`, `EnvironmentFile=/opt/beacon/.env`,
  `ExecStart=/opt/beacon/webapp --port 8000 … --api-dsn https://beacon.seilbekskindirov.dev/`.
- Acceptance: one unit referencing only `/opt/beacon`, flat beacon names, the
  prod domain.
- Complexity: Easy.

### Task 4: nginx — Cloudflare edge (replace kz.be-happy set)
- Description: delete the `nginx.kz_behappy*.conf` family + `certbot.com_lingocrm.sh`;
  add `configs/nginx.beacon.conf` (single upstream `beacon_web`→8000, one
  server block: 80→443 redirect, Cloudflare origin cert, Authenticated Origin
  Pulls, include common settings), `configs/nginx.beacon_common_settings.conf`
  (listen 443 ssl http2, client_max_body_size, gzip include, content-hashed
  asset cache location, `location /` proxy), `configs/nginx.beacon_gzip.conf`.
- Acceptance: `nginx -t` valid on host; hashed-asset cache + gzip preserved;
  the prod domain routes to loopback 8000.
- Pitfalls: hashed-asset regex location MUST stay above `location /`. Origin
  cert + AOP files are operator-placed; deploy guards on their presence.
- Complexity: Medium.

### Task 5: Makefile
- Description: replace `deploy_environments` with a single `init` target that
  provisions the host — set `/opt/beacon` to `root:github_aide 2775` (so the CI
  deploy user can write it), install `beacon.service` + `sqlite_dump.sh`, ship
  `configs/nginx.beacon*.conf`, fetch the Cloudflare origin-pull CA, enable the
  vhost, `nginx -t` + reload. Update `clean` and `backups` paths (`/opt/beacon`,
  `beacon.*.sqlite`).
- Acceptance: `make init` provisions the host; targets reference only flat
  beacon names/paths.
- Complexity: Medium.

### Task 6: CI workflows (vpntunnel model)
- Description: drop `stage.yml`; add `ci.main.yml` (lint+test+sanity build on
  push/PR to `main`); rename `prime.yml` → `release.yml` triggered on `r_*`
  release tags (tag validation, single `PRIME` env, bare binary names
  `webapp`/`collector`/`notifier`/`migrator`, `REMOTE_ENV_FILE=/opt/beacon/.env`,
  `SERVICE_NAME=beacon`).
  (GitHub `vars` `REMOTE_DIR`→`/opt/beacon` and `SSH_*` are operator-set.)
- Acceptance: `main` push runs tests only; an `r_*` tag deploys; both files are
  valid YAML and reference only flat beacon names.
- Complexity: Medium.

### Task 7: backup scripts + env example
- Description: `configs/sqlite_dump.sh` + `sqlite_dump.env.example`:
  `/opt/monitor`→`/opt/beacon`, `*_monitor.sqlite`→`*_beacon.sqlite`,
  `GDRIVE_REMOTE` default → beacon path, host comment. `.env.example`:
  `monitor.db`→`beacon.db`. Delete the dead `deploy/cron/sqlite-backup.sh`.
- Acceptance: no `/opt/monitor` or `*_monitor.sqlite` left in active scripts.
- Complexity: Easy.

### Task 8: Docs prose
- Description: `README.md`, `CLAUDE.md`, `cmd/doctor/README.md`,
  `deploy/README.md`, `.claude/agents/gocode-forecaster.md` — project name,
  `/opt/beacon`, service/binary/env/DB names, nginx config paths, the new
  domains, BotFather Menu Button URL, `webAppURL`.
- Acceptance: active docs describe beacon + new domains; no stale
  `be-happy.kz` / `/opt/monitor` / `kz.be-happy` references in active docs.
- Complexity: Medium.

### Task 9: Server-side migration runbook
- Description: produce an operator runbook (chat + `deploy/README.md` section)
  covering: DNS/Cloudflare setup, origin cert + origin-pull CA placement,
  `/opt/monitor`→`/opt/beacon` move, DB/env-file rename, systemd unit
  replacement, cron-wrapper rename, GitHub `vars`/`secrets` updates, BotFather
  URL, and the old-vhost teardown.
- Acceptance: a step-by-step list the operator can execute.
- Complexity: Medium.

## Execution Order
1 → 2 → 3 → 7 → 4 → 5 → 6 → 8 → (make test) → 9.

## Risks
- A stray module-path miss breaks the build — caught by `go build ./...`.
- Over-eager substitution renaming the English word "monitor" in comments or the
  external `prorochestvo` dep — mitigated by scoping seds and reviewing brand
  edits by hand.
- Server migration is destructive (data move, unit replacement, DNS cutover) —
  documented as a runbook for the operator, not automated.

## Trade-offs
- Dropping Let's Encrypt/certbot for Cloudflare origin certs removes renewal
  cron but adds a Cloudflare dependency (matches vpntunnel, operator's choice).
- Collapsing to single-prod (no stage) drops a whole environment's config/CI
  surface; pre-prod validation moves to local runs against a pulled prod
  snapshot + an on-demand throwaway bot.

## Server migration runbook (one-time, operator-run on the host)

The repo changes are deployed by `make init` and the CI, but the live host
carries data and units under the old `monitor`
names. The old box ran prime+stage; this migration keeps **prime only** and
retires stage. Migrate in this order. SSH alias `be-happy.kz` is unchanged.

### 1. Cloudflare (zone `seilbekskindirov.dev`)
- Add a **proxied** (orange-cloud) DNS record `beacon` pointing at the host IP.
  SSL/TLS mode **Full (strict)**.
- Reuse the existing `*.seilbekskindirov.dev` Cloudflare **Origin** certificate
  (already placed for vpntunnel at
  `/etc/nginx/certificates/cloudflare/seilbekskindirov.dev.{pem,key}`). If absent,
  create one in the Cloudflare dashboard and place it (key mode 0600).
- (Optional) enable Authenticated Origin Pulls on the zone; nginx already
  requires the Cloudflare client cert.

### 2. GitHub repo settings
- `REMOTE_DIR=/opt/beacon` (repo or PRIME var). `SSH_HOSTNAME/PORT/USERNAME` and
  secrets unchanged.
- **PRIME → Deployment branches and tags** already allows `r_*` tags, which is the
  release-tag scheme `release.yml` triggers on — no policy change needed. Optionally
  add a required-reviewer rule to gate the deploy. The `STAGE` environment is unused.

### 3. Host: stop everything
```bash
sudo systemctl stop prime_monitor stage_monitor
# pause the collector/notifier cron wrappers (comment the crontab entries or
# rename the *.sh wrappers) so nothing fires mid-migration
```

### 4. Host: data + env (prime's, already migrated to /opt/beacon)
- `/opt/beacon/beacon.sqlite` and `/opt/beacon/.env` carry prime's data. Edit
  `.env`: `SQLITEDB_DSN=sqlite://_:_@_:_//opt/beacon/beacon.sqlite`;
  `TELEGRAMBOT_DSN`/`PROXY_URL` unchanged (swap the bot token for a fresh prod bot
  if desired). Retire any stage leftovers: `sudo rm -f /opt/beacon/stage_monitor* /opt/beacon/.stage_monitor.env`.

### 5. Host: bootstrap the release layout from the on-disk binaries
So the service can start before the first CI deploy, seed one artifact set:
```bash
cd /opt/beacon
VID="$(date -u +%Y%m%d%H%M%S)-r_0.0.1"
sudo install -d -o github_aide -g github_aide -m 0755 artifacts bin "artifacts/$VID"
sudo mv webapp collector notifier migrator "artifacts/$VID/"
sudo chown github_aide:github_aide "artifacts/$VID"/* && sudo chmod +x "artifacts/$VID"/*
sudo ln -sfn "../artifacts/$VID" bin/release
```
- Point the cron wrappers `collector.sh`/`notifier.sh` at
  `/opt/beacon/bin/release/{collector,notifier}` (and the right `.env`/logs dir);
  keep them `root:root 0750`. The new `sqlite_dump.sh` snapshots
  `/opt/beacon/beacon.sqlite`; update `GDRIVE_REMOTE` in `/opt/beacon/backups/.env`
  if you want the Drive path renamed off `…/monitor`.

### 6. Provision the host (from the local repo)
```bash
make init   # re-own base dir root:root 0755 (CI confined to artifacts/+bin/),
            # install beacon.service + beacon-migrate.service + sudoers + nginx vhost/CA
```

### 7. Host: enable the unit, retire the old ones
```bash
sudo systemctl disable --now prime_monitor stage_monitor 2>/dev/null || true
sudo rm -f /etc/systemd/system/prime_monitor.service /etc/systemd/system/stage_monitor.service
sudo systemctl daemon-reload
sudo systemctl enable --now beacon       # bin/release/webapp exists from step 5
# re-point + re-enable the cron wrappers
```
Future releases are hands-off: a `v*` tag uploads a new `artifacts/<VID>/`, flips
`bin/release`, runs `beacon-migrate`, restarts `beacon`, health-gates + rolls back.

### 7a. (Recommended) de-root — see deploy/README.md "Hardening"
Create a `beacon` system user, move the DB to `/var/lib/beacon`, set `User=beacon`
on both units. Caps a leaked CI key at "runs as beacon", never root.

### 8. Host: retire the old kz.be-happy nginx vhost
```bash
sudo rm -f /etc/nginx/sites-enabled/kz.be-happy* \
           /etc/nginx/sites-available/kz.be-happy \
           /etc/nginx/snippets/kz.be-happy.* \
           /etc/nginx/configurations/kz.be-happy.* \
           /etc/nginx/certificates/kz.be-happy.conf
sudo nginx -t && sudo systemctl reload nginx
```
- **Caution:** the box is shared (also hosts lingocrm). Confirm nothing else
  still serves `be-happy.kz` before removing its DNS / Let's Encrypt renewal.
  The retired certbot script `certbot.com_lingocrm.sh` covered the be-happy.kz
  cert — only drop that renewal if be-happy.kz is fully decommissioned.

### 9. BotFather
- Set the Menu Button URL to `https://beacon.seilbekskindirov.dev/` (trailing
  slash) for the production bot.

### 10. Verify
```bash
sudo systemctl is-active beacon
curl -fs https://beacon.seilbekskindirov.dev/healthz
# in the unit log, confirm: "settings: webAppURL=https://beacon.seilbekskindirov.dev/"
# and the "hashed assets: app=… wasm_exec=…" line
```

### First release after migration
- Cut a release tag to trigger `release.yml`: `git tag r_0.0.1 && git push origin r_0.0.1`.

### Repo follow-up the operator must do by hand
- `.env.example` line 2: `monitor.db` → `beacon.db` (the `.env*` path is blocked
  by the agent's permission guard).
