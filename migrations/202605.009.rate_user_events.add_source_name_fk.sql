-- 202605.009: add FK rate_user_events.source_name -> rate_sources(name) ON DELETE CASCADE,
-- and drop the legacy NOT NULL DEFAULT '' constraint so the FK enforces only
-- on non-NULL values.
--
-- Migration 004 added source_name with DEFAULT '' so existing rows could be
-- backfilled without breaking inserts. Empty string is not a valid FK target,
-- so the rebuild converts any '' values to NULL and removes the default.
-- SQLite skips FK enforcement when the column is NULL; production code paths
-- (collector and notifier) always set a non-empty source_name, so the only
-- callers that hit the NULL path are tests that do not exercise source binding.
--
-- See 202605.007 for the table-rebuild rationale.

CREATE TABLE rate_user_events_new (
	id          TEXT NOT NULL PRIMARY KEY,
	user_type    TEXT NOT NULL,
	user_id      TEXT NOT NULL,
	message     TEXT NOT NULL,
	status      TEXT NOT NULL DEFAULT 'pending',
	sent_at      TEXT,
	last_error   TEXT NOT NULL DEFAULT '',
	created_at   TEXT NOT NULL,
	source_name  TEXT REFERENCES rate_sources(name) ON DELETE CASCADE
);

INSERT INTO rate_user_events_new
	(id, user_type, user_id, message, status, sent_at, last_error, created_at, source_name)
	SELECT id, user_type, user_id, message, status, sent_at, last_error, created_at,
	       NULLIF(source_name, '')
	FROM rate_user_events;

DROP TABLE rate_user_events;

ALTER TABLE rate_user_events_new RENAME TO rate_user_events;

CREATE INDEX IF NOT EXISTS idx_rate_user_events_status  ON rate_user_events (status);
CREATE INDEX IF NOT EXISTS idx_rate_user_events_user    ON rate_user_events (user_type, user_id);
CREATE INDEX IF NOT EXISTS idx_rate_user_events_created ON rate_user_events (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_rate_user_events_failed  ON rate_user_events (created_at DESC) WHERE status = 'failed';
CREATE INDEX IF NOT EXISTS idx_rate_user_events_source  ON rate_user_events (source_name);
