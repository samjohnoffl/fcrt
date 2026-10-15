-- 000003_experiment_layer.up.sql
-- Adds persisted experiment pair records with hashes for reproducibility.
-- Both members of a pair share the same control_hash and manifest_hash,
-- enforced at the application layer and verifiable by comparison.

BEGIN;

-- experiment_pair: the top-level record for one baseline+variant comparison.
-- Encodes everything needed to replay the experiment deterministically.
CREATE TABLE experiment_pair (
  id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  -- Human label for the pair.
  name              TEXT        NOT NULL,
  -- Foreign key to the locked cohort that was used.
  cohort_id         UUID        NOT NULL REFERENCES cohort(id),
  -- SHA-256 of the ControlConfig JSON (with config_hash="").
  -- Both runs in the pair must carry the same value.
  control_hash      TEXT        NOT NULL,
  -- SHA-256 of the CohortManifest JSON used as input.
  -- Ensures both runs saw the same frozen dataset.
  manifest_hash     TEXT        NOT NULL,
  -- Generator seed recorded from the manifest for traceability.
  seed              BIGINT      NOT NULL,
  -- Declared change applied to the variant (human description).
  declared_change   TEXT        NOT NULL,
  -- MutationType constant applied, e.g. "split_transaction".
  mutation_type     TEXT        NOT NULL,
  -- Full mutation parameters as JSON.
  mutation_spec     JSONB       NOT NULL DEFAULT '{}',
  -- Lifecycle status.
  status            TEXT        NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending','running','completed','failed')),
  -- Error message if status = 'failed'.
  error_msg         TEXT,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON experiment_pair (cohort_id, status);
CREATE TRIGGER experiment_pair_updated_at
  BEFORE UPDATE ON experiment_pair
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

COMMENT ON COLUMN experiment_pair.control_hash IS
  'SHA-256 of ControlConfig canonical JSON. Pair runs are comparable only if hashes match.';
COMMENT ON COLUMN experiment_pair.manifest_hash IS
  'SHA-256 of CohortManifest JSON. Detects if the frozen input context changed between runs.';

-- experiment_run: one execution (baseline or variant) within a pair.
-- Replaces the generic experiment_run table for the new typed schema.
-- (The original experiment_run from migration 000001 is retained for
--  legacy compatibility; this is a new, richer table.)
CREATE TABLE experiment_pair_run (
  id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  pair_id       UUID        NOT NULL REFERENCES experiment_pair(id),
  run_type      TEXT        NOT NULL CHECK (run_type IN ('baseline','variant')),
  -- control_hash is redundant here (same as pair) but recorded for
  -- self-contained auditability of the run record.
  control_hash  TEXT        NOT NULL,
  manifest_hash TEXT        NOT NULL,
  alert_count   INT         NOT NULL DEFAULT 0,
  -- rule_id → alert count map, e.g. {"structuring_v1": 2}
  alert_summary JSONB       NOT NULL DEFAULT '{}',
  -- Full Alert list, serialised as JSON.
  raw_alerts    JSONB       NOT NULL DEFAULT '[]',
  started_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  completed_at  TIMESTAMPTZ
);
CREATE INDEX ON experiment_pair_run (pair_id, run_type);

COMMENT ON COLUMN experiment_pair_run.run_type IS
  'baseline: unmodified input. variant: input with mutation_spec applied.';

-- experiment_outcome: the diff between a pair's two runs.
-- Computed once on pair completion and stored for fast retrieval.
CREATE TABLE experiment_outcome (
  id                    UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
  pair_id               UUID    NOT NULL REFERENCES experiment_pair(id) UNIQUE,
  baseline_run_id       UUID    NOT NULL REFERENCES experiment_pair_run(id),
  variant_run_id        UUID    NOT NULL REFERENCES experiment_pair_run(id),
  -- baseline_alerts - variant_alerts; positive = alerts dropped (evasion).
  evasion_delta         INT     NOT NULL,
  -- Rules newly triggered in the variant that were absent in baseline.
  new_alert_rule_ids    TEXT[]  NOT NULL DEFAULT '{}',
  -- Rules present in baseline but absent in variant.
  dropped_alert_rule_ids TEXT[] NOT NULL DEFAULT '{}',
  -- Whether the variant produced an identical alert set to the baseline.
  outcome_unchanged     BOOLEAN NOT NULL,
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON COLUMN experiment_outcome.outcome_unchanged IS
  'True when evasion_delta=0 and new/dropped rule sets are both empty. '
  'Used in regression tests to assert a mutation had no effect.';

COMMIT;
