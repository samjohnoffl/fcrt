-- 000004_org_id.down.sql
-- Revert DECISION-003: remove org_id columns and organisations table.
BEGIN;

-- Drop foreign keys and columns from all tables.
ALTER TABLE job DROP CONSTRAINT IF EXISTS fk_job_org;
ALTER TABLE job DROP COLUMN IF EXISTS org_id;

ALTER TABLE evidence_bundle DROP CONSTRAINT IF EXISTS fk_evidence_bundle_org;
ALTER TABLE evidence_bundle DROP COLUMN IF EXISTS org_id;

ALTER TABLE rule_coverage DROP CONSTRAINT IF EXISTS fk_rule_coverage_org;
ALTER TABLE rule_coverage DROP COLUMN IF EXISTS org_id;

ALTER TABLE experiment_diff DROP CONSTRAINT IF EXISTS fk_experiment_diff_org;
ALTER TABLE experiment_diff DROP COLUMN IF EXISTS org_id;

ALTER TABLE experiment_run DROP CONSTRAINT IF EXISTS fk_experiment_run_org;
ALTER TABLE experiment_run DROP COLUMN IF EXISTS org_id;

ALTER TABLE experiment DROP CONSTRAINT IF EXISTS fk_experiment_org;
ALTER TABLE experiment DROP COLUMN IF EXISTS org_id;

ALTER TABLE scenario_proposal DROP CONSTRAINT IF EXISTS fk_scenario_proposal_org;
ALTER TABLE scenario_proposal DROP COLUMN IF EXISTS org_id;

ALTER TABLE bank_transaction DROP CONSTRAINT IF EXISTS fk_bank_transaction_org;
ALTER TABLE bank_transaction DROP COLUMN IF EXISTS org_id;

ALTER TABLE bank_account DROP CONSTRAINT IF EXISTS fk_bank_account_org;
ALTER TABLE bank_account DROP COLUMN IF EXISTS org_id;

ALTER TABLE bank_entity DROP CONSTRAINT IF EXISTS fk_bank_entity_org;
ALTER TABLE bank_entity DROP COLUMN IF EXISTS org_id;

ALTER TABLE cohort DROP CONSTRAINT IF EXISTS fk_cohort_org;
ALTER TABLE cohort DROP COLUMN IF EXISTS org_id;

-- Drop organisations table and its trigger.
DROP TRIGGER IF EXISTS organisations_updated_at ON organisations;
DROP TABLE IF EXISTS organisations;

COMMIT;
