-- 000010_discovery.down.sql
BEGIN;

DROP INDEX IF EXISTS experiment_pair_proposal_idx;
ALTER TABLE experiment_pair DROP COLUMN IF EXISTS scenario_proposal_id;

DROP TABLE IF EXISTS discovery_candidate;
DROP TABLE IF EXISTS discovery_run;

COMMIT;
