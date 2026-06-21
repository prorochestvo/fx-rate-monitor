#!/usr/bin/env bash
#
# sqlite_dump.sh — snapshot the prime + stage SQLite databases into
# /opt/monitor/backups on the host, then mirror them to Google Drive via rclone.
# Runs on be-happy.kz, not locally.
#
# Install: scp this file to /opt/monitor/backups/sqlite_dump.sh, chmod +x, then add a
# daily 00:00 crontab entry (run `crontab -e` as the service user, root here):
#
#   0 0 * * * /opt/monitor/backups/sqlite_dump.sh > /opt/monitor/logs/backup.log 2>&1
#
# Each run writes <env>_monitor.<YYYYMMDD>.sqlite and copies new snapshots to
# GDRIVE_REMOTE. Two independent retention windows:
#   - local host: LOCAL_RETENTION_DAYS  (default 7)  — short, the disk is scarce
#   - Google Drive: REMOTE_RETENTION_DAYS (default 14) — the long-term archive
# rclone must be configured with a remote named "gdrive" (see `rclone config`).
#
# Optional overrides live in an untracked .env next to this script
# (/opt/monitor/backups/.env). It is sourced before the defaults below, so it can
# set GDRIVE_REMOTE, LOCAL_RETENTION_DAYS or REMOTE_RETENTION_DAYS. Example line:
#   REMOTE_RETENTION_DAYS=30
# rclone needs no override here: it auto-discovers ~/.config/rclone/rclone.conf of
# the user the cron runs as.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "${SCRIPT_DIR}/.env" ]; then
    set -a
    . "${SCRIPT_DIR}/.env"
    set +a
fi

MONITOR_DIR="/opt/monitor"
BACKUP_DIR="${MONITOR_DIR}/backups"
GDRIVE_REMOTE="${GDRIVE_REMOTE:-gdrive:backups/kz_behappy/monitor}"
LOCAL_RETENTION_DAYS="${LOCAL_RETENTION_DAYS:-7}"
REMOTE_RETENTION_DAYS="${REMOTE_RETENTION_DAYS:-14}"
SNAPSHOT_GLOB='*_monitor.*.sqlite*'
STAMP="$(date -u +%Y%m%d)"

mkdir -p "${BACKUP_DIR}"

for env in prime stage; do
    src="${MONITOR_DIR}/${env}_monitor.sqlite"
    dst="${BACKUP_DIR}/${env}_monitor.${STAMP}.sqlite"

    if [ ! -f "${src}" ]; then
        echo "skip: ${src} not present"
        continue
    fi

    if command -v sqlite3 >/dev/null 2>&1; then
        # Online backup: a consistent snapshot even while the services write
        # (the databases run in WAL mode).
        sqlite3 "${src}" ".backup '${dst}'"
    else
        # Fallback when the sqlite3 CLI is absent: copy the main file plus its
        # WAL/SHM sidecars so the snapshot can be replayed consistently.
        cp "${src}" "${dst}"
        [ -f "${src}-wal" ] && cp "${src}-wal" "${dst}-wal" || true
        [ -f "${src}-shm" ] && cp "${src}-shm" "${dst}-shm" || true
    fi
    echo "backup: ${dst}"
done

# Mirror new snapshots to Google Drive. `copy` is additive — it never deletes
# upstream — so the remote keeps its own (longer) retention independent of the
# host. Filtered to snapshot files so this script itself is not uploaded.
if command -v rclone >/dev/null 2>&1; then
    rclone copy "${BACKUP_DIR}" "${GDRIVE_REMOTE}" --include "${SNAPSHOT_GLOB}"
    rclone delete "${GDRIVE_REMOTE}" --include "${SNAPSHOT_GLOB}" --min-age "${REMOTE_RETENTION_DAYS}d"
    echo "synced to ${GDRIVE_REMOTE} (remote retention: ${REMOTE_RETENTION_DAYS}d)"
else
    echo "WARNING: rclone not found — keeping local snapshots only, no Google Drive copy"
fi

# Local retention: keep only the last LOCAL_RETENTION_DAYS days on the host.
find "${BACKUP_DIR}" -maxdepth 1 -type f -name "${SNAPSHOT_GLOB}" \
    -mtime "+${LOCAL_RETENTION_DAYS}" -print -delete
