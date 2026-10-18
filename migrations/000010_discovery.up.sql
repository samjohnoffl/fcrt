-- 000010_discovery.up.sql
--
-- Adaptive discovery: persist the candidate-selection decision, not just its
-- outcome. Each run records the strategy, the versioned weights, the seed, the
-- budget and the full scoring trace for every selected candidate, so a reviewer
-- can answer "why was this tested next?" without re-running a model.
--
-- Also links an experiment pair to the proposal it came from. Without that
-- link the loop cannot tell which proposed behaviours have already been tried.
BEGIN;

CREATE TABLE IF NOT EXISTS discovery_run (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    cohort_id       UUID        NOT NULL REFERENCES cohort(id),
    strategy        TEXT        NOT NULL
                                CHECK (strategy IN ('adaptive','random','gemini_only')),
    weights_version TEXT        NOT NULL,
    weights         JSONB       NOT NULL,
    seed            BIGINT      NOT NULL,
    budget          INTEGER     NOT NULL CHECK (budget > 0),
    pool_size       INTEGER     NOT NULL DEFAULT 0,
    valid_pool_size INTEGER     NOT NULL DEFAULT 0,
    notes           TEXT        NOT NULL DEFAULT '',
    duration_ms     BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS discovery_run_cohort_idx
    ON discovery_run (cohort_id, strategy, created_at DESC);

CREATE TABLE IF NOT EXISTS discovery_candidate (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id               UUID        NOT NULL REFERENCES discovery_run(id) ON DELETE CASCADE,
    scenario_proposal_id UUID        NOT NULL REFERENCES scenario_proposal(id),
    mutation_type        TEXT        NOT NULL,
    genome               JSONB       NOT NULL,
    genome_valid         BOOLEAN     NOT NULL,
    score_c              NUMERIC     NOT NULL,
    score_n              NUMERIC     NOT NULL,
    score_i              NUMERIC     NOT NULL,
    score_v              NUMERIC     NOT NULL,
    total_score          NUMERIC     NOT NULL,
    rank                 INTEGER     NOT NULL,
    selected             BOOLEAN     NOT NULL,
    rationale            TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS discovery_candidate_run_idx
    ON discovery_candidate (run_id, rank);

ALTER TABLE experiment_pair
    ADD COLUMN IF NOT EXISTS scenario_proposal_id UUID REFERENCES scenario_proposal(id);

CREATE INDEX IF NOT EXISTS experiment_pair_proposal_idx
    ON experiment_pair (scenario_proposal_id);

COMMIT;
