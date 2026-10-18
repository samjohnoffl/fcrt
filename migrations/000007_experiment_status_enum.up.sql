-- 000007_experiment_status_enum.up.sql
--
-- Keep the experiment_pair.status CHECK constraint in sync with the Go Status
-- enum in internal/experiment/types.go, which was extended with 'queued',
-- 'cancelled', and 'inconclusive' (adaptive campaigns / benchmarks). The DB
-- constraint from 000003 only allowed ('pending','running','completed','failed'),
-- so writing any new status would fail at the database.
BEGIN;

ALTER TABLE experiment_pair DROP CONSTRAINT IF EXISTS experiment_pair_status_check;
ALTER TABLE experiment_pair ADD CONSTRAINT experiment_pair_status_check
  CHECK (status IN (
    'pending', 'queued', 'running', 'completed',
    'failed', 'cancelled', 'inconclusive'
  ));

COMMIT;
