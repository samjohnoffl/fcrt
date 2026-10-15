-- 000002_generator_version.up.sql
-- Adds generator_version, manifest and batch selection fields to cohort.
-- Adds a balance_cents ledger column to bank_account.

BEGIN;

-- Record which version of the generator produced the cohort data.
-- Enables reproducibility checks: same seed + same generator_version → same data.
ALTER TABLE cohort
  ADD COLUMN generator_version TEXT NOT NULL DEFAULT '',
  ADD COLUMN manifest          JSONB NOT NULL DEFAULT '{}',
  ADD COLUMN window_start      TIMESTAMPTZ,
  ADD COLUMN window_end        TIMESTAMPTZ;

COMMENT ON COLUMN cohort.generator_version IS
  'Semver string of the bank Generator that produced this cohort. '
  'Must match when reproducing for regression.';

COMMENT ON COLUMN cohort.manifest IS
  'CohortManifest JSON: entity_count, account_count, tx_count, '
  'window_start, window_end, generator_version, seed, cohort_id.';

COMMENT ON COLUMN cohort.window_start IS
  'Inclusive start of the 90-day (configurable) transaction time window.';

COMMENT ON COLUMN cohort.window_end IS
  'Exclusive end of the transaction time window.';

-- Exact integer balance tracking in the smallest currency unit (e.g. cents).
-- Stored as BIGINT to avoid floating-point rounding entirely.
-- Positive = credit balance; generator enforces non-negative at each step.
ALTER TABLE bank_account
  ADD COLUMN balance_cents BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN bank_account.balance_cents IS
  'Running ledger balance in integer smallest-unit (cents). '
  'Generator keeps this >= 0 at all times. currency column gives the ISO code.';

-- Index to support batch-selection queries by time window.
CREATE INDEX ON bank_transaction (cohort_id, tx_timestamp)
  WHERE tx_timestamp IS NOT NULL;  -- partial; belt-and-suspenders
  
COMMIT;
