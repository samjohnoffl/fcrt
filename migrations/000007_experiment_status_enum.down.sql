-- 000007_experiment_status_enum.down.sql
-- Revert to the original 000003 status set. Fails if rows use the newer statuses.
BEGIN;

ALTER TABLE experiment_pair DROP CONSTRAINT IF EXISTS experiment_pair_status_check;
ALTER TABLE experiment_pair ADD CONSTRAINT experiment_pair_status_check
  CHECK (status IN ('pending', 'running', 'completed', 'failed'));

COMMIT;
