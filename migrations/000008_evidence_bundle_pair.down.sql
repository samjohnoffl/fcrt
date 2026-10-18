-- 000008_evidence_bundle_pair.down.sql
-- Revert: point evidence_bundle back at the legacy `experiment` table.
BEGIN;

ALTER TABLE evidence_bundle DROP CONSTRAINT IF EXISTS uq_evidence_bundle_experiment;
ALTER TABLE evidence_bundle DROP CONSTRAINT IF EXISTS fk_evidence_bundle_pair;
DROP INDEX IF EXISTS evidence_bundle_experiment_created_idx;

ALTER TABLE evidence_bundle
  ADD CONSTRAINT evidence_bundle_experiment_id_fkey
  FOREIGN KEY (experiment_id) REFERENCES experiment(id);

COMMIT;
