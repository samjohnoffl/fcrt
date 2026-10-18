-- 000004_org_id.up.sql
-- DECISION-003: Multi-organization isolation (option B — add org_id to all tables).
-- Adds org_id FK to every table for forward compatibility. Backfilled with
-- a single default org for existing rows.
BEGIN;

-- Utility: auto-update updated_at trigger already exists from 000001_init.

-- -------------------------------------------------------
-- Create organisations table
-- -------------------------------------------------------
CREATE TABLE organisations (
  id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name        TEXT        NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER organisations_updated_at
  BEFORE UPDATE ON organisations
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- Insert a default org for backfilling existing rows.
INSERT INTO organisations (id, name)
VALUES (gen_random_uuid(), 'default')
ON CONFLICT DO NOTHING;

-- -------------------------------------------------------
-- Add org_id to all existing tables
-- -------------------------------------------------------

ALTER TABLE cohort ADD COLUMN org_id UUID NOT NULL;
ALTER TABLE cohort ADD CONSTRAINT fk_cohort_org
  FOREIGN KEY (org_id) REFERENCES organisations(id);
CREATE INDEX ON cohort (org_id);
UPDATE cohort SET org_id = (SELECT id FROM organisations WHERE name = 'default' LIMIT 1);

ALTER TABLE bank_entity ADD COLUMN org_id UUID NOT NULL;
ALTER TABLE bank_entity ADD CONSTRAINT fk_bank_entity_org
  FOREIGN KEY (org_id) REFERENCES organisations(id);
CREATE INDEX ON bank_entity (org_id);
UPDATE bank_entity SET org_id = (SELECT id FROM organisations WHERE name = 'default' LIMIT 1);

ALTER TABLE bank_account ADD COLUMN org_id UUID NOT NULL;
ALTER TABLE bank_account ADD CONSTRAINT fk_bank_account_org
  FOREIGN KEY (org_id) REFERENCES organisations(id);
CREATE INDEX ON bank_account (org_id);
UPDATE bank_account SET org_id = (SELECT id FROM organisations WHERE name = 'default' LIMIT 1);

ALTER TABLE bank_transaction ADD COLUMN org_id UUID NOT NULL;
ALTER TABLE bank_transaction ADD CONSTRAINT fk_bank_transaction_org
  FOREIGN KEY (org_id) REFERENCES organisations(id);
CREATE INDEX ON bank_transaction (org_id);
UPDATE bank_transaction SET org_id = (SELECT id FROM organisations WHERE name = 'default' LIMIT 1);

ALTER TABLE scenario_proposal ADD COLUMN org_id UUID NOT NULL;
ALTER TABLE scenario_proposal ADD CONSTRAINT fk_scenario_proposal_org
  FOREIGN KEY (org_id) REFERENCES organisations(id);
CREATE INDEX ON scenario_proposal (org_id);
UPDATE scenario_proposal SET org_id = (SELECT id FROM organisations WHERE name = 'default' LIMIT 1);

ALTER TABLE experiment ADD COLUMN org_id UUID NOT NULL;
ALTER TABLE experiment ADD CONSTRAINT fk_experiment_org
  FOREIGN KEY (org_id) REFERENCES organisations(id);
CREATE INDEX ON experiment (org_id);
UPDATE experiment SET org_id = (SELECT id FROM organisations WHERE name = 'default' LIMIT 1);

ALTER TABLE experiment_run ADD COLUMN org_id UUID NOT NULL;
ALTER TABLE experiment_run ADD CONSTRAINT fk_experiment_run_org
  FOREIGN KEY (org_id) REFERENCES organisations(id);
CREATE INDEX ON experiment_run (org_id);
UPDATE experiment_run SET org_id = (SELECT id FROM organisations WHERE name = 'default' LIMIT 1);

ALTER TABLE experiment_diff ADD COLUMN org_id UUID NOT NULL;
ALTER TABLE experiment_diff ADD CONSTRAINT fk_experiment_diff_org
  FOREIGN KEY (org_id) REFERENCES organisations(id);
CREATE INDEX ON experiment_diff (org_id);
UPDATE experiment_diff SET org_id = (SELECT id FROM organisations WHERE name = 'default' LIMIT 1);

ALTER TABLE rule_coverage ADD COLUMN org_id UUID NOT NULL;
ALTER TABLE rule_coverage ADD CONSTRAINT fk_rule_coverage_org
  FOREIGN KEY (org_id) REFERENCES organisations(id);
CREATE INDEX ON rule_coverage (org_id);
UPDATE rule_coverage SET org_id = (SELECT id FROM organisations WHERE name = 'default' LIMIT 1);

ALTER TABLE evidence_bundle ADD COLUMN org_id UUID NOT NULL;
ALTER TABLE evidence_bundle ADD CONSTRAINT fk_evidence_bundle_org
  FOREIGN KEY (org_id) REFERENCES organisations(id);
CREATE INDEX ON evidence_bundle (org_id);
UPDATE evidence_bundle SET org_id = (SELECT id FROM organisations WHERE name = 'default' LIMIT 1);

ALTER TABLE job ADD COLUMN org_id UUID NOT NULL;
ALTER TABLE job ADD CONSTRAINT fk_job_org
  FOREIGN KEY (org_id) REFERENCES organisations(id);
CREATE INDEX ON job (org_id);
UPDATE job SET org_id = (SELECT id FROM organisations WHERE name = 'default' LIMIT 1);

COMMIT;
