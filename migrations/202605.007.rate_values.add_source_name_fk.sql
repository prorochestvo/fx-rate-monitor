-- 202605.007: add FK rate_values.source_name -> rate_sources(name) ON DELETE CASCADE.
-- SQLite cannot ALTER TABLE ADD CONSTRAINT, so the table is rebuilt.
-- INSERT INTO ... SELECT triggers FK validation; an orphan source_name would
-- abort the migration loudly, which is the desired failure mode.
-- Plan: plans/008-multi-agent-audit-followup.md Task 2.

CREATE TABLE rate_values_new (
	id              TEXT NOT NULL PRIMARY KEY,
	source_name     TEXT NOT NULL REFERENCES rate_sources(name) ON DELETE CASCADE,
	base_currency   TEXT NOT NULL,
	quote_currency  TEXT NOT NULL,
	price           REAL NOT NULL,
	timestamp       TEXT NOT NULL
);

INSERT INTO rate_values_new (id, source_name, base_currency, quote_currency, price, timestamp)
	SELECT id, source_name, base_currency, quote_currency, price, timestamp
	FROM rate_values;

DROP TABLE rate_values;

ALTER TABLE rate_values_new RENAME TO rate_values;

CREATE INDEX IF NOT EXISTS idx_rate_values_lookup ON rate_values (source_name, base_currency, quote_currency, timestamp DESC);
