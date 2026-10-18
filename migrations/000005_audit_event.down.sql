-- 000005_audit_event.down.sql
-- Revert DECISION-006: drop audit_event table.
BEGIN;

DROP TABLE IF EXISTS audit_event;

COMMIT;