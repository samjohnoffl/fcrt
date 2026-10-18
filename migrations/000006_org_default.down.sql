-- 000006_org_default.down.sql
-- Reverts 000006: drop the 'default' org column DEFAULT from all org_id columns.
BEGIN;

DO $$
DECLARE
  tbl    TEXT;
  tables TEXT[] := ARRAY[
    'cohort', 'bank_entity', 'bank_account', 'bank_transaction',
    'scenario_proposal', 'experiment', 'experiment_run', 'experiment_diff',
    'rule_coverage', 'evidence_bundle', 'job', 'audit_event'
  ];
BEGIN
  FOREACH tbl IN ARRAY tables LOOP
    IF to_regclass(tbl) IS NOT NULL THEN
      EXECUTE format('ALTER TABLE %I ALTER COLUMN org_id DROP DEFAULT', tbl);
    END IF;
  END LOOP;
END $$;

COMMIT;
