-- 000011_control_outcomes.up.sql
--
-- Adds the five-status control outcome model and the three coverage maps:
--   * control coverage  -> rule_coverage (extended with non-trigger statuses)
--   * outcome coverage  -> outcome_coverage (rule x status cross-tab)
--   * scenario coverage -> scenario_coverage (genome dimension x value)
--
-- Every evaluation persists one rule_outcome row per control family, including
-- controls that did not trigger and controls that could not be evaluated.

ALTER TABLE rule_coverage
    ADD COLUMN IF NOT EXISTS times_not_triggered  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS times_not_applicable INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS times_inconclusive   INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS times_failed         INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_status          TEXT,
    ADD COLUMN IF NOT EXISTS last_evaluated_at    TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS rule_outcome (
    id           UUID PRIMARY KEY,
    run_id       UUID NOT NULL REFERENCES experiment_pair_run(id) ON DELETE CASCADE,
    pair_id      UUID NOT NULL REFERENCES experiment_pair(id) ON DELETE CASCADE,
    rule_id      TEXT NOT NULL,
    rule_version TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN
        ('triggered', 'not_triggered', 'not_applicable', 'inconclusive', 'failed')),
    reason_code  TEXT NOT NULL,
    event_ids    UUID[] NOT NULL DEFAULT '{}',
    account_ids  UUID[] NOT NULL DEFAULT '{}',
    entity_ids   UUID[] NOT NULL DEFAULT '{}',
    amount_cents BIGINT NOT NULL DEFAULT 0,
    currency     TEXT NOT NULL DEFAULT '',
    detail       TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS rule_outcome_run_idx  ON rule_outcome (run_id);
CREATE INDEX IF NOT EXISTS rule_outcome_pair_idx ON rule_outcome (pair_id);
CREATE INDEX IF NOT EXISTS rule_outcome_rule_idx ON rule_outcome (rule_id, status);

CREATE TABLE IF NOT EXISTS outcome_coverage (
    rule_id      TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN
        ('triggered', 'not_triggered', 'not_applicable', 'inconclusive', 'failed')),
    times_seen   INTEGER NOT NULL DEFAULT 0,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (rule_id, status)
);

CREATE TABLE IF NOT EXISTS scenario_coverage (
    dimension    TEXT NOT NULL,
    value        TEXT NOT NULL,
    times_seen   INTEGER NOT NULL DEFAULT 0,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (dimension, value)
);
