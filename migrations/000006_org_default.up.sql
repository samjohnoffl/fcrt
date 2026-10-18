-- 000006_org_default.up.sql
--
-- Fix for DECISION-003 fallout: migration 000004 added `org_id NOT NULL` to
-- every table, but the existing write paths were never updated to populate it,
-- so every INSERT failed with a not-null violation against a migrated database.
--
-- The platform has no auth→organisation mapping yet (multi-tenancy is
-- explicitly "forward compatibility" per 000004). Until that exists, we attach
-- the single 'default' organisation as the column DEFAULT so that writes which
-- omit org_id land in the default org. Application code that knows the real org
-- can still set org_id explicitly and override the default.
--
-- This preserves the schema (column stays NOT NULL, FK + index intact) and
-- keeps the path to real multi-tenancy open.
BEGIN;

DO $$
DECLARE
  def_org UUID;
  tbl     TEXT;
  tables  TEXT[] := ARRAY[
    'cohort', 'bank_entity', 'bank_account', 'bank_transaction',
    'scenario_proposal', 'experiment', 'experiment_run', 'experiment_diff',
    'rule_coverage', 'evidence_bundle', 'job', 'audit_event'
  ];
BEGIN
  -- Resolve the backfill org created by 000004; create it if somehow absent.
  SELECT id INTO def_org
    FROM organisations
   WHERE name = 'default'
   ORDER BY created_at
   LIMIT 1;

  IF def_org IS NULL THEN
    INSERT INTO organisations (name) VALUES ('default') RETURNING id INTO def_org;
  END IF;

  FOREACH tbl IN ARRAY tables LOOP
    -- Skip tables that do not exist in this deployment (legacy schemas).
    IF to_regclass(tbl) IS NOT NULL THEN
      EXECUTE format('ALTER TABLE %I ALTER COLUMN org_id SET DEFAULT %L', tbl, def_org);
    END IF;
  END LOOP;
END $$;

COMMIT;
