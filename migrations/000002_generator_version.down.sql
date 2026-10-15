-- 000002_generator_version.down.sql

BEGIN;

DROP INDEX IF EXISTS bank_transaction_cohort_id_tx_timestamp_idx1;

ALTER TABLE bank_account
  DROP COLUMN IF EXISTS balance_cents;

ALTER TABLE cohort
  DROP COLUMN IF EXISTS generator_version,
  DROP COLUMN IF EXISTS manifest,
  DROP COLUMN IF EXISTS window_start,
  DROP COLUMN IF EXISTS window_end;

COMMIT;
