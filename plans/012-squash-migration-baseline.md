# Task Breakdown

## Overview

Collapse the SQLite migration history from 10 files (001-010) to 7 by folding
the three FK-cascade rebuilds (current 007, 008, 009) directly into the
respective `*_initiate.sql` migrations they patch, and renumber the trailing
`rate_user_profiles` migration (current 010) into the gap.

This is a deliberate, one-time deviation from the project rule that "once
applied to any production database the filename is immutable". The
authorisation conditions are:

- No production environment exists yet — only a single stage instance.
- The operator (sole stage owner) has explicitly authorised wiping the stage
  database and letting the next deploy repopulate `__schema_migrations` from
  the new baseline.
- Cost of the deviation: one `rm <stage.db>` and one redeploy.
- Cost of NOT doing it: three permanent `fix-my-previous-migration` files in
  history that have to be re-read by every future contributor.

Target end state in `migrations/`:

```
202605.001.rate_sources.table_initiate.sql              unchanged
202605.002.rate_values.table_initiate.sql               current 002 + FK from 007
202605.003.rate_user_subscriptions.table_initiate.sql   current 003 + FK from 008
202605.004.rate_user_events.table_initiate.sql          current 004 + nullable FK from 009
202605.005.execution_history.table_initiate.sql         unchanged
202605.006.rate_user_profiles.table_initiate.sql        current 010 body, renamed
202605.007.rate_sources.seed_initial.sql                current 006 body, renamed
```

The seed stays last because it INSERTs into tables that the four `_initiate`
migrations create. Inside the seed file order is also load-bearing:
`rate_sources` rows are inserted before `rate_user_subscriptions` rows, so the
new inline FK on `rate_user_subscriptions.source_name` is satisfied at
INSERT-time on a fresh baseline.

## Assumptions

- Single operator. No other engineer or CI environment has applied the current
  010 migration set against a long-lived database that we care about.
- Stage DB will be wiped and replayed from scratch; no data preservation needed.
- The final schema after the squash must be byte-for-byte equivalent to the
  schema produced by replaying all 10 current migrations against an empty DB
  (same columns, same nullability, same defaults, same indexes — including the
  partial `WHERE status = 'failed'` index on `rate_user_events`).
- `make test` exercises the full migration set in-memory via
  `internal/infrastructure/sqlitedb/sqlitedbtest/apply.go`, so any schema
  divergence will surface as a repository-test failure.
- The `internal/application/sourceaudit/seedparser_test.go` reference to
  `202605.014.*.sql` is a hypothetical example string (no such file exists);
  it is not affected by the renumbering.

## Tasks

### Task 1: Rewrite `202605.002.rate_values.table_initiate.sql` with inline FK

- **Description:** Replace the body of
  `migrations/202605.002.rate_values.table_initiate.sql` with the
  post-rebuild schema produced by current 007. The new file declares
  `source_name TEXT NOT NULL REFERENCES rate_sources(name) ON DELETE CASCADE`
  on the CREATE TABLE statement instead of patching it later. The index
  `idx_rate_values_lookup` is retained verbatim (it is present in both the
  current 002 and current 007).
- **Acceptance Criteria:**
  - [ ] `migrations/202605.002.rate_values.table_initiate.sql` contains
        exactly one `CREATE TABLE IF NOT EXISTS rate_values (...)` block.
  - [ ] `source_name` column is declared as
        `TEXT NOT NULL REFERENCES rate_sources(name) ON DELETE CASCADE`.
  - [ ] No `_new` rename dance, no `DROP TABLE`, no `INSERT INTO ... SELECT`.
  - [ ] `idx_rate_values_lookup` is created with the same column list and
        ordering as before:
        `(source_name, base_currency, quote_currency, timestamp DESC)`.
  - [ ] File has no comment block explaining a "rebuild" — this is now the
        original definition.
- **Pitfalls & edge cases:**
  - Forgetting `ON DELETE CASCADE` silently turns a destructive operator
    delete into an FK error. Compare against current 007 line by line.
  - SQLite stores FK definitions in the table-level sqlite_master entry. If
    the operator forgets to wipe the stage DB, `__schema_migrations` will say
    002 already ran with the OLD body — re-running this new 002 would be a
    no-op due to `IF NOT EXISTS` and the FK would never appear. The wipe is
    not optional; document it in Task 10.
- **Complexity:** Easy
- **Code Example:**

```sql
CREATE TABLE IF NOT EXISTS rate_values (
    id              TEXT NOT NULL PRIMARY KEY,
    source_name     TEXT NOT NULL REFERENCES rate_sources(name) ON DELETE CASCADE,
    base_currency   TEXT NOT NULL,
    quote_currency  TEXT NOT NULL,
    price           REAL NOT NULL,
    timestamp       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rate_values_lookup ON rate_values (source_name, base_currency, quote_currency, timestamp DESC);
```

### Task 2: Rewrite `202605.003.rate_user_subscriptions.table_initiate.sql` with inline FK + sourceName index

- **Description:** Replace the body of
  `migrations/202605.003.rate_user_subscriptions.table_initiate.sql` with the
  post-rebuild schema from current 008. Declare
  `source_name TEXT NOT NULL REFERENCES rate_sources(name) ON DELETE CASCADE`.
  Carry over ALL four indexes — the three from current 003 (`usrSubscriptions`,
  `userType`, `userID`) plus `idx_rate_user_subscriptions_sourceName` that
  current 008 added.
- **Acceptance Criteria:**
  - [ ] `source_name` declared as
        `TEXT NOT NULL REFERENCES rate_sources(name) ON DELETE CASCADE`.
  - [ ] All `NOT NULL DEFAULT` values on `condition_type`, `condition_value`,
        `latest_notified_rate` are preserved verbatim.
  - [ ] Four indexes present:
        `idx_rate_user_subscriptions_usrSubscriptions`,
        `idx_rate_user_subscriptions_userType`,
        `idx_rate_user_subscriptions_userID`,
        `idx_rate_user_subscriptions_sourceName`.
  - [ ] No `_new` rename dance.
- **Pitfalls & edge cases:**
  - Forgetting the fourth index (`sourceName`) — it is not in current 003,
    only in current 008. Easy miss if you start from 003 and only diff
    against the FK line.
- **Complexity:** Easy

### Task 3: Rewrite `202605.004.rate_user_events.table_initiate.sql` with nullable FK + all five indexes

- **Description:** Replace the body of
  `migrations/202605.004.rate_user_events.table_initiate.sql` with the
  post-rebuild schema from current 009. The `source_name` column is the
  trickiest: current 004 declared it `NOT NULL DEFAULT ''`; current 009 made
  it nullable (no NOT NULL, no DEFAULT) and added the FK. The new 004 must
  declare it as
  `source_name TEXT REFERENCES rate_sources(name) ON DELETE CASCADE`
  — no NOT NULL, no DEFAULT.

  Carry over ALL five indexes — `status`, `user`, `created`, the partial
  `failed` index (`WHERE status = 'failed'`), and the `source` index added by
  current 009.
- **Acceptance Criteria:**
  - [ ] `source_name` declared as
        `TEXT REFERENCES rate_sources(name) ON DELETE CASCADE`
        with no `NOT NULL`, no `DEFAULT ''`.
  - [ ] `sent_at` remains nullable (`TEXT` with no NOT NULL), unchanged from
        original 004.
  - [ ] Five indexes present, with the `failed` one keeping its partial
        `WHERE status = 'failed'` clause.
  - [ ] No `NULLIF` backfill INSERT — the squashed migration runs on a fresh
        DB, there is nothing to backfill.
- **Pitfalls & edge cases:**
  - Mistakenly keeping `NOT NULL DEFAULT ''` on `source_name` is the most
    likely error. The Go code in `internal/repository/rateuserevent.go` writes
    NULL (not '') for unbound events; a NOT NULL column would reject those
    inserts at runtime.
  - Dropping the partial index `idx_rate_user_events_failed` would silently
    slow down failed-events queries. Diff index list against current 009.
- **Complexity:** Medium

### Task 4: Rename `202605.010.rate_user_profiles.table_initiate.sql` to `202605.006.rate_user_profiles.table_initiate.sql`

- **Description:** Move the file via `git mv`. Content is unchanged. This
  closes the numeric gap left by deleting 007/008/009 in Task 6 and keeps
  `rate_user_profiles` ahead of the seed (which becomes 007 in Task 5).
- **Acceptance Criteria:**
  - [ ] `migrations/202605.006.rate_user_profiles.table_initiate.sql` exists
        with the exact contents of current
        `migrations/202605.010.rate_user_profiles.table_initiate.sql`.
  - [ ] `migrations/202605.010.rate_user_profiles.table_initiate.sql` no
        longer exists.
  - [ ] `git mv` preserves history (verify via `git log --follow`).
- **Pitfalls & edge cases:**
  - Using `cp` + `rm` instead of `git mv` loses follow-able history. Use
    `git mv`.
- **Complexity:** Easy

### Task 5: Rename `202605.006.rate_sources.seed_initial.sql` to `202605.007.rate_sources.seed_initial.sql`

- **Description:** Move the file via `git mv`. Content is unchanged. The seed
  must remain LAST so all four `_initiate` tables exist before the INSERTs run.
  Sequencing within the seed file itself (`rate_sources` INSERTs precede
  `rate_user_subscriptions` INSERTs) is unchanged and required because the new
  003 declares an inline FK from `rate_user_subscriptions.source_name`.
- **Acceptance Criteria:**
  - [ ] `migrations/202605.007.rate_sources.seed_initial.sql` exists with the
        exact contents of current
        `migrations/202605.006.rate_sources.seed_initial.sql`.
  - [ ] `migrations/202605.006.rate_sources.seed_initial.sql` no longer exists
        (the slot is now occupied by `rate_user_profiles`).
  - [ ] Verify the in-file ordering invariant: `INSERT INTO rate_sources`
        rows precede `INSERT INTO rate_user_subscriptions` rows. (Spot-check;
        current file has 36 rate_sources INSERTs followed by 19 subscription
        INSERTs.)
- **Pitfalls & edge cases:**
  - Tasks 4 and 5 must NOT both be in-flight simultaneously in the same
    working tree before commit — at one point both would target the literal
    file name `202605.006.*.sql`. Apply them sequentially with a commit
    boundary, or apply Task 4 first (frees up the 006 slot, then Task 5 can
    move 006-seed to 007 — except Task 5 moves the now-current 006-seed which
    is fine because Task 4 already moved 010 to 006-rate_user_profiles… wait,
    that collides on 006). Execute in this order: do Task 5 FIRST (006-seed
    becomes 007-seed, slot 006 is free), THEN Task 4 (010-profiles becomes
    006-profiles).
- **Complexity:** Easy

### Task 6: Delete obsolete migration files

- **Description:** Remove the three FK-fix-up migrations now that their schema
  has been folded into 002/003/004. Files to delete:
  - `migrations/202605.007.rate_values.add_source_name_fk.sql`
  - `migrations/202605.008.rate_user_subscriptions.add_source_name_fk.sql`
  - `migrations/202605.009.rate_user_events.add_source_name_fk.sql`
- **Acceptance Criteria:**
  - [ ] All three files removed via `git rm`.
  - [ ] `ls migrations/*.sql` returns exactly 7 files matching the target end
        state listed in the Overview.
  - [ ] `embed.go` still compiles (it embeds the directory, not individual
        files, so no edit needed — but build it to confirm).
- **Pitfalls & edge cases:**
  - If Task 1/2/3 weren't completed correctly and these files are deleted
    anyway, the schema regresses to the pre-FK version. Run the test suite
    (Task 9) before considering this task done.
- **Complexity:** Easy

### Task 7: Update Go-code comments referencing the old migration filenames

- **Description:** Three locations in production and test code mention
  migration `202605.008` or `202605.009` by name. After the squash these
  filenames no longer exist; the FK constraints and the nullable column now
  live in the original `_initiate` migrations. Rewrite the comments.

  Locations:
  1. `internal/repository/rateuserevent.go` near line 329 — currently:
     `// source_name became nullable in migration 202605.009. Empty string would`
     New text should explain that source_name IS nullable (without the
     historical narrative) and that empty-string would violate the FK.
  2. `internal/repository/rateuserevent.go` near lines 480-482 — currently:
     `// rateUserEventSqlSelect emits IFNULL(source_name, '') so callers can scan / into a non-pointer string field; migration 202605.009 made the column / nullable to support events created before a source was bound.`
     Rewrite to describe the current state without referencing the migration
     number.
  3. `internal/repository/main_test.go` near lines 24 and 66 — both currently
     say "FK added in migration 202605.008". Rewrite to "FK on
     `rate_user_subscriptions.source_name`" (no migration number) — the
     reader doesn't need history, just the invariant.

  Locations NOT to touch:
  - `internal/application/rulegen/plausibility.go:18` and its test (`plausibility_test.go:16`) reference
    `migrations/202605.007.rate_sources.seed_initial.sql`. After Task 5 the
    seed lives at exactly `202605.007.rate_sources.seed_initial.sql` — the
    reference stays valid by coincidence. Verify the path with `ls` and leave
    the comment alone.
  - `internal/application/sourceaudit/seedparser_test.go:142` references a
    hypothetical `202605.014.*.sql` that does not exist in the tree. Leave
    alone.
- **Acceptance Criteria:**
  - [ ] No file under `internal/` matches `grep -rn '202605\.008\|202605\.009' internal/`.
  - [ ] `grep -rn '202605\.007' internal/` returns only the two
        plausibility references, both still pointing at
        `migrations/202605.007.rate_sources.seed_initial.sql` which exists
        after Task 5.
  - [ ] Replacement comments describe the invariant (nullable column, FK
        present) without the historical "added in migration X" framing.
  - [ ] `make build` succeeds (comments must not break a doc-comment-formatter
        if one is wired into vet).
- **Pitfalls & edge cases:**
  - Don't silently rewrite a comment to lose useful information. The original
    comments justify WHY the column is nullable / why the FK is enforced —
    keep that reasoning, just drop the "migration 202605.009" provenance.
  - The two plausibility references can survive a future renumbering of the
    seed only if we leave them as a documented coincidence. Add a one-line
    note in each plausibility comment: "see migrations/ for current seed
    filename" so a future rename doesn't blindside the reader. Optional but
    recommended.
- **Complexity:** Easy
- **Code Example:**

```go
// Before:
// source_name became nullable in migration 202605.009. Empty string would
// violate the FK (no rate_source has name=''); send NULL instead.
sourceName := sourceNameForDB(record.SourceName)

// After:
// source_name is nullable and carries an FK to rate_sources(name). Empty
// string would violate the FK (no rate_source has name=''); send NULL when
// the event is not bound to a source.
sourceName := sourceNameForDB(record.SourceName)
```

### Task 8: Add migration-immutability carve-out note to CLAUDE.md

- **Description:** The "Database" section of `CLAUDE.md` (around line 158)
  states: "Once applied to any production database the filename is immutable:
  renaming triggers a duplicate apply." This squash is a deliberate
  one-time exception. Add a short note inline (do not invent a new section)
  that records the carve-out without weakening the general rule.

  The note should:
  - Acknowledge the rule still applies to all FUTURE migrations.
  - Record that the 001-010 → 001-007 baseline reset happened with operator
    authorisation because no production environment existed yet.
  - Document the recovery procedure for any DB that already applied the old
    set: drop the DB file, redeploy, let `cmd/migrator` repopulate.
- **Acceptance Criteria:**
  - [ ] `CLAUDE.md` "Database" section retains the original immutability
        statement verbatim.
  - [ ] A short paragraph (≤ 6 lines) is appended IMMEDIATELY after the
        immutability paragraph, marked clearly as a one-time exception
        (e.g. "Historical exception (2026-05-31):").
  - [ ] The paragraph names the affected files and the recovery procedure.
  - [ ] No other section of `CLAUDE.md` is edited.
- **Pitfalls & edge cases:**
  - Don't relax the immutability rule itself ("immutable except when
    convenient"). Future contributors must read it as ironclad; the carve-out
    is the closed exception, not a precedent.
- **Complexity:** Easy

### Task 9: Run `make test` and confirm a green tree

- **Description:** With all migration edits and comment updates in place, run
  the full test suite. The in-memory test harness applies the squashed
  migration set from scratch on every test, so any schema divergence will
  appear here.
- **Acceptance Criteria:**
  - [ ] `make test` exits zero.
  - [ ] No new test is skipped or removed as a result of the squash.
  - [ ] `internal/application/sourceaudit/seedparser_count_test.go` still
        passes — the glob `*.seed*.sql` matches exactly one file (the renamed
        seed at 007), same count as before.
  - [ ] `internal/repository/...` repository tests pass (these exercise the
        FK and the nullable `source_name` indirectly through
        `seedRateSources` and the cascade-delete tests).
- **Pitfalls & edge cases:**
  - If a test fails with `FOREIGN KEY constraint failed` during seed insertion,
    the most likely cause is index ordering within Task 2/3 or a missing
    `REFERENCES` clause. Re-diff against current 007/008/009.
  - If `idx_rate_user_events_failed` is missing the `WHERE status = 'failed'`
    clause, no test will fail (it's still a valid index), but query plans
    diverge. Confirm visually via `.schema rate_user_events` in a scratch
    SQLite shell built from the new migrations.
- **Complexity:** Easy

### Task 10: Document the stage-wipe procedure for the operator

- **Description:** This is documentation only — not executed by the engineer.
  In the plan completion notes (or a follow-up `ops/` doc if the project
  has one — check before creating), record the exact procedure the operator
  follows after this PR lands:

  1. Merge the squash PR into the deploy branch (stage).
  2. SSH to the stage host.
  3. Read the `SQLITEDB_DSN` value from the systemd `EnvironmentFile` for
     `cmd/web` (e.g. `sqlite:///var/lib/fxmonitor/stage.db`).
  4. Stop the three services: `cmd/web`, `cmd/collector`, `cmd/notifier`.
  5. `rm <path-from-DSN>` and `rm <path>-shm <path>-wal` if WAL files exist.
  6. Trigger the standard stage deploy. The CI workflow
     `.github/workflows/stage.yml` runs `cmd/migrator` over SSH before
     restarting the services, which populates a fresh `__schema_migrations`
     from the squashed file set.
  7. Verify post-deploy: `sqlite3 <db> 'SELECT filename FROM __schema_migrations'`
     should return exactly 7 rows matching the new filenames.
- **Acceptance Criteria:**
  - [ ] Procedure is documented in the plan file or a project ops doc — not
        only in the orchestrator's reply.
  - [ ] Procedure names the exact services to stop before the wipe.
  - [ ] Procedure includes the post-deploy verification step.
- **Pitfalls & edge cases:**
  - Forgetting the `-shm` / `-wal` sidecars after `rm` of the main DB leaves
    SQLite in a confused state on next open. Include them in the wipe.
  - If the services are not stopped before the wipe, the collector/notifier
    write paths will recreate empty tables (no migrations applied), then
    `cmd/migrator` will re-run all 7 files and `__schema_migrations` ends up
    inconsistent. Stopping the units first is mandatory.
- **Complexity:** Easy

## Execution Order

Execute strictly in this order. Several tasks have file-name collisions if
parallelised, and Task 9 must run last to verify the whole stack.

1. Task 5 (rename current 006-seed → 007-seed) — frees the 006 slot.
2. Task 4 (rename current 010-profiles → 006-profiles) — fills the freed slot.
3. Task 1 (rewrite 002-rate_values with inline FK).
4. Task 2 (rewrite 003-rate_user_subscriptions with inline FK + sourceName index).
5. Task 3 (rewrite 004-rate_user_events with nullable FK + five indexes).
6. Task 6 (delete obsolete 007/008/009 .add_source_name_fk.sql files).
7. Task 7 (update Go-code comments).
8. Task 8 (add CLAUDE.md carve-out note).
9. Task 9 (run `make test`).
10. Task 10 (document stage-wipe procedure).

Parallelisable: Tasks 1, 2, 3 can in principle run in parallel because they
edit different files, but execution by a single engineer in the listed order
is safer (each edit is small and the test pass at the end catches any
mistake). Tasks 7 and 8 are also independent of each other and of 1/2/3 but
sequential execution is simplest.

## Risks

- **Anyone with an applied DB needs a wipe.** Operator (sole stage owner) has
  pre-authorised. No local-dev databases need preservation — devs use
  in-memory or throwaway files. If a contributor later joins the project and
  has a persisted dev DB, they hit the same wipe-or-debug choice; the
  CLAUDE.md carve-out note documents the fix.
- **FK orphan check is lost.** The current 007/008/009 migrations validate
  FK targets at table-rebuild time via `INSERT INTO ... SELECT`, which would
  loudly fail on orphan rows. The squashed migrations skip this because they
  run against an empty DB. This is FINE for a fresh baseline but means the
  schema squash CANNOT be applied to any DB containing the
  pre-2026-05-21 orphan subscription for `KZ_NATIONALBANK_BID_EUR_KZT` (the
  bug that current 008's PRE-CONDITION comment guards against). Document
  in Task 10 that the operator should wipe rather than attempt to migrate
  in place.
- **Seed-file invariant: rate_sources INSERTs precede subscription INSERTs.**
  The new 003 declares an inline FK from
  `rate_user_subscriptions.source_name` to `rate_sources(name)`. On a fresh
  baseline the seed in the new 007 runs as a single migration; if any future
  edit reorders its INSERT statements, FK enforcement fires immediately.
  Spot-check after Task 5 and leave a one-line comment at the top of the seed
  file calling out the ordering requirement.
- **CLAUDE.md immutability rule erosion.** Adding a carve-out near the
  immutability statement risks future contributors reading it as
  "immutability is negotiable". Phrase the carve-out as a CLOSED, dated
  exception (Task 8) — not as guidance for future squashes.
- **Test harness uses `embed.FS`, not the live filesystem.** `embed.go`
  embeds the entire `migrations/` directory at compile time, so renamed
  files are picked up automatically on rebuild. No `embed.go` edit needed,
  but `make test` (which rebuilds) is the only signal that the renames took
  effect. Don't trust `go test` from a stale build cache — clean first if in
  doubt.
- **Plausibility table reference is currently coincidence-safe.** Tasks 5
  moves the seed to `202605.007.rate_sources.seed_initial.sql`, which happens
  to match the existing string literal in
  `internal/application/rulegen/plausibility.go:18`. A future renumbering
  would silently break this comment. Task 7 mitigates by adding "see
  migrations/ for current seed filename" to make the dependency explicit.

## Trade-offs

- **Squash vs. live-with-the-cruft.** We deviate from "applied migrations are
  immutable" once, with operator authorisation. Cost: one stage DB wipe and
  one redeploy. Benefit: three fewer files in the migration history,
  permanently. The cleaner baseline is worth the one-time hit because there
  is no production environment to coordinate with.

- **Per-table squash (Option A) vs. mega-baseline.** Folding each FK fix-up
  into its respective `_initiate.sql` keeps the file-per-table mental model
  and leaves seed/profiles untouched. A "single 001.baseline.sql" approach
  would be tidier on paper but would erase the table-by-table chronology
  inside the migration file and make future diffs harder to read. Option A
  is the smaller deviation.

- **Inline FK declarations vs. SQLite ALTER TABLE ADD CONSTRAINT
  workaround.** SQLite does not support `ALTER TABLE ADD CONSTRAINT`, which
  is why current 007/008/009 exist as table-rebuild dances in the first
  place. Inline declarations in the squashed `_initiate.sql` files sidestep
  the entire problem — there is no "previous version" to ALTER from, so the
  FK simply IS the table definition.

- **Comment updates over historical preservation.** Task 7 strips
  "migration 202605.009" from production code comments. We lose the historical
  breadcrumb. The trade-off: the comments are about the CURRENT invariant
  (nullable column, FK present), not project history. Anyone genuinely
  curious about the squash can read `git log -- migrations/` or this plan
  file.

- **Carve-out note in CLAUDE.md vs. silent override.** Adding a six-line
  note increases CLAUDE.md noise but makes the deviation auditable. Silent
  override is faster but leaves future contributors confused when they
  notice the immutability rule and the obviously-squashed history don't
  match. The note pays for itself the first time someone asks "why is 010
  missing".

## Stage-wipe procedure (operator runbook)

After this squash lands on the deploy branch, any stage DB that previously
applied the old 10-file set must be wiped before the next deploy. Steps:

1. Merge the squash PR into the stage deploy branch.
2. SSH to the stage host.
3. Read `SQLITEDB_DSN` from the systemd `EnvironmentFile` for `cmd/web`
   (e.g. `sqlite:///var/lib/fxmonitor/stage.db`).
4. Stop all three services:
   ```
   systemctl stop fxmonitor-web fxmonitor-collector fxmonitor-notifier
   ```
   Stopping first is mandatory — if any service writes after the wipe, it
   recreates tables without migrations applied and `cmd/migrator` ends up in
   an inconsistent state.
5. Delete the DB file and its WAL sidecars:
   ```
   rm /var/lib/fxmonitor/stage.db
   rm -f /var/lib/fxmonitor/stage.db-shm /var/lib/fxmonitor/stage.db-wal
   ```
6. Trigger the standard stage deploy. The CI workflow
   `.github/workflows/stage.yml` runs `cmd/migrator` over SSH before
   restarting the services, which populates a fresh `__schema_migrations`
   from the 7-file baseline.
7. Verify post-deploy:
   ```
   sqlite3 /var/lib/fxmonitor/stage.db \
     'SELECT filename FROM __schema_migrations ORDER BY filename'
   ```
   Expected output — exactly 7 rows:
   ```
   202605.001.rate_sources.table_initiate.sql
   202605.002.rate_values.table_initiate.sql
   202605.003.rate_user_subscriptions.table_initiate.sql
   202605.004.rate_user_events.table_initiate.sql
   202605.005.execution_history.table_initiate.sql
   202605.006.rate_user_profiles.table_initiate.sql
   202605.007.rate_sources.seed_initial.sql
   ```
