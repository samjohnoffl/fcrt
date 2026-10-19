-- 000013_review_decision.up.sql
--
-- Reviewer decisions on completed experiments. One decision per pair; the
-- rationale is required so a rejection is explainable and auditable.

CREATE TABLE IF NOT EXISTS review_decision (
    id         UUID PRIMARY KEY,
    pair_id    UUID NOT NULL REFERENCES experiment_pair(id) ON DELETE CASCADE,
    reviewer   TEXT NOT NULL,
    decision   TEXT NOT NULL CHECK (decision IN ('approved', 'rejected', 'escalated')),
    rationale  TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (pair_id)
);

CREATE INDEX IF NOT EXISTS review_decision_decision_idx ON review_decision (decision);
