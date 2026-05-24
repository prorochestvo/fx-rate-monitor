#!/bin/bash
#
# sqlite-backup.sh — nightly online backup of the monitor SQLite database.
#
# Usage (invoke from cron):
#   ENV_FILE=/opt/monitor/.stage_monitor.env  \
#   BACKUP_DIR=/var/backups/monitor/stage     \
#   RETENTION_DAYS=14                         \
#   /opt/monitor/sqlite-backup.sh
#
# Reads SQLITEDB_DSN (`sqlite://<path>`) from ENV_FILE and runs
# `sqlite3 <path> ".backup <BACKUP_DIR>/<env>.<YYYY-MM-DD>.sqlite"`.
# Old backups beyond RETENTION_DAYS are deleted.
#
# Exit codes:
#   0  — backup completed and retention pruned
#   1  — env file unreadable / SQLITEDB_DSN missing / sqlite3 not found
#   2  — backup command failed
#
# The sqlite3 .backup command holds a brief shared lock on the source
# file; concurrent writers retry per busy_timeout=5000 (set by the
# service at open time). It is safe to run while collector / notifier /
# webapp are live.

set -euo pipefail

: "${ENV_FILE:?ENV_FILE is required}"
: "${BACKUP_DIR:?BACKUP_DIR is required}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"

if [[ ! -r "$ENV_FILE" ]]; then
    echo "sqlite-backup: cannot read env file $ENV_FILE" >&2
    exit 1
fi

# shellcheck disable=SC1090
set -a
. "$ENV_FILE"
set +a

if [[ -z "${SQLITEDB_DSN:-}" ]]; then
    echo "sqlite-backup: SQLITEDB_DSN is empty in $ENV_FILE" >&2
    exit 1
fi

DB_PATH="${SQLITEDB_DSN#sqlite://}"
if [[ ! -r "$DB_PATH" ]]; then
    echo "sqlite-backup: cannot read database $DB_PATH" >&2
    exit 1
fi

if ! command -v sqlite3 >/dev/null 2>&1; then
    echo "sqlite-backup: sqlite3 binary not found in PATH" >&2
    exit 1
fi

mkdir -p "$BACKUP_DIR"
TS="$(date +%Y-%m-%d)"
ENV_NAME="$(basename "$ENV_FILE" .env)"
ENV_NAME="${ENV_NAME#.}"
OUT="$BACKUP_DIR/${ENV_NAME}.${TS}.sqlite"

# The output path is interpolated into a sqlite3 dot-command (`.backup '$OUT'`),
# which is parsed by SQLite's shell — not by /bin/sh. Single quotes terminate
# the argument and spaces split it, so reject both to keep the command safe.
if [[ "$OUT" == *\'* ]]; then
    echo "sqlite-backup: backup path must not contain a single quote: $OUT" >&2
    exit 1
fi
if [[ "$OUT" == *" "* ]]; then
    echo "sqlite-backup: backup path must not contain spaces: $OUT" >&2
    exit 1
fi

# .backup is the SQLite online-backup API — atomic snapshot, safe with
# live writers. Output is a complete standalone database file.
if ! sqlite3 "$DB_PATH" ".backup '$OUT'"; then
    echo "sqlite-backup: backup failed for $DB_PATH → $OUT" >&2
    exit 2
fi

echo "sqlite-backup: wrote $OUT ($(stat -c%s "$OUT" 2>/dev/null || stat -f%z "$OUT") bytes)"

# Retention prune. -mtime +N matches files older than N*24h.
find "$BACKUP_DIR" -maxdepth 1 -type f -name "${ENV_NAME}.*.sqlite" \
    -mtime "+$RETENTION_DAYS" -print -delete

exit 0
