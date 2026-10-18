-- 000005_audit_event.up.sql
-- DECISION-006: Audit logging (option B — dedicated audit_event table in PostgreSQL).
-- Records approval/rejection events for scenario proposals and experiment execution.
BEGIN;

-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- -------------------------------------------------------
-- audit_event
-- -------------------------------------------------------
CREATE TABLE audit_event (
  id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          UUID         NOT NULL REFERENCES organisations(id),
  event_type      TEXT         NOT NULL, -- 'scenario_approved' | 'scenario_rejected' | 'experiment_started' | 'experiment_completed' | 'experiment_failed'
  actor_uid       TEXT         NOT NULL, -- Firebase UID of the user who triggered the event
  target_type     TEXT         NOT NULL, -- 'scenario_proposal' | 'experiment' | 'cohort'
  target_id       UUID         NOT NULL,
  details         JSONB        NOT NULL DEFAULT '{}', -- event-specific payload
  ip_address      TEXT,
  user_agent      TEXT,
  created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX ON audit_event (org_id, created_at DESC);
CREATE INDEX ON audit_event (target_type, target_id);
CREATE INDEX ON audit_event (event_type, created_at DESC);

-- Add trigger for updated_at (though audit events are immutable, kept for consistency)
-- Not needed for audit events as they are append-only.

COMMIT;