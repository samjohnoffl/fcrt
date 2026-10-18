-- 000008_evidence_bundle_pair.up.sql
--
-- PRODUCT_SPEC item 7 requires evidence bundles to be stored in PostgreSQL
-- (JSONB). The 000001 evidence_bundle table references the *legacy* `experiment`
-- table, but experiments moved to `experiment_pair` in 000003 and the legacy
-- table is never written — so no bundle could ever be persisted (FK violation).
--
-- This migration re-points the foreign key at experiment_pair, enforces one
-- bundle per experiment, and adds a lookup index.
BEGIN;

-- Drop whichever FK currently constrains evidence_bundle.experiment_id
-- (auto-named by Postgres in 000001).
DO $$
DECLARE
  cname TEXT;
BEGIN
  SELECT conname INTO cname
    FROM pg_constraint
   WHERE conrelid = 'evidence_bundle'::regclass
     AND contype = 'f'
     AND conname <> 'fk_evidence_bundle_org'  -- keep the org FK from 000004
   LIMIT 1;
  IF cname IS NOT NULL THEN
    EXECUTE format('ALTER TABLE evidence_bundle DROP CONSTRAINT %I', cname);
  END IF;
END $$;

ALTER TABLE evidence_bundle
  ADD CONSTRAINT fk_evidence_bundle_pair
  FOREIGN KEY (experiment_id) REFERENCES experiment_pair(id) ON DELETE CASCADE;

-- One immutable bundle per experiment (upsert target).
ALTER TABLE evidence_bundle
  ADD CONSTRAINT uq_evidence_bundle_experiment UNIQUE (experiment_id);

CREATE INDEX IF NOT EXISTS evidence_bundle_experiment_created_idx
  ON evidence_bundle (experiment_id, created_at DESC);

COMMIT;
