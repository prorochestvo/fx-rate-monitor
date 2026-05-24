-- 202605.008: add FK rate_user_subscriptions.source_name -> rate_sources(name) ON DELETE CASCADE.
-- See 202605.007 for the rebuild rationale.
--
-- PRE-CONDITION: every rate_user_subscriptions.source_name must reference a
-- name present in rate_sources. If migrator fails here with
-- "FOREIGN KEY constraint failed", run on the target DB before re-deploying:
--
--   DELETE FROM rate_user_subscriptions
--      WHERE source_name NOT IN (SELECT name FROM rate_sources);
--
-- This unblocks DBs that applied the pre-2026-05-21 content of 202605.006,
-- which contained an orphan subscription for KZ_NATIONALBANK_BID_EUR_KZT.

CREATE TABLE rate_user_subscriptions_new (
	id                   TEXT NOT NULL PRIMARY KEY,
	user_type            TEXT NOT NULL,
	user_id              TEXT NOT NULL,
	source_name          TEXT NOT NULL REFERENCES rate_sources(name) ON DELETE CASCADE,
	condition_type       TEXT NOT NULL DEFAULT 'delta',
	condition_value      TEXT NOT NULL DEFAULT '10',
	latest_notified_rate REAL NOT NULL DEFAULT 0,
	updated_at           TEXT NOT NULL,
	created_at           TEXT NOT NULL
);

INSERT INTO rate_user_subscriptions_new
	(id, user_type, user_id, source_name, condition_type, condition_value, latest_notified_rate, updated_at, created_at)
	SELECT id, user_type, user_id, source_name, condition_type, condition_value, latest_notified_rate, updated_at, created_at
	FROM rate_user_subscriptions;

DROP TABLE rate_user_subscriptions;

ALTER TABLE rate_user_subscriptions_new RENAME TO rate_user_subscriptions;

CREATE INDEX IF NOT EXISTS idx_rate_user_subscriptions_usrSubscriptions ON rate_user_subscriptions (user_type, user_id);
CREATE INDEX IF NOT EXISTS idx_rate_user_subscriptions_userType ON rate_user_subscriptions (user_type);
CREATE INDEX IF NOT EXISTS idx_rate_user_subscriptions_userID ON rate_user_subscriptions (user_id);
CREATE INDEX IF NOT EXISTS idx_rate_user_subscriptions_sourceName ON rate_user_subscriptions (source_name);
