-- 000001_init.up.sql
-- Initial schema for Financial Crime AI Red Team
-- Run via: migrate -path ./migrations -database $DATABASE_URL up

BEGIN;

-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- -------------------------------------------------------
-- Utility: auto-update updated_at
-- -------------------------------------------------------
CREATE OR REPLACE FUNCTION trigger_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- -------------------------------------------------------
-- cohort
-- -------------------------------------------------------
CREATE TABLE cohort (
  id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  name        TEXT        NOT NULL,
  predicate   JSONB       NOT NULL DEFAULT '{}',
  seed        BIGINT      NOT NULL,
  locked_at   TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER cohort_updated_at
  BEFORE UPDATE ON cohort
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- -------------------------------------------------------
-- bank_entity
-- -------------------------------------------------------
CREATE TABLE bank_entity (
  id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  cohort_id    UUID        NOT NULL REFERENCES cohort(id),
  entity_type  TEXT        NOT NULL CHECK (entity_type IN ('individual','company','shell')),
  name         TEXT        NOT NULL,
  country_code CHAR(2)     NOT NULL,
  risk_score   NUMERIC(5,4) NOT NULL DEFAULT 0.0 CHECK (risk_score >= 0 AND risk_score <= 1),
  attributes   JSONB       NOT NULL DEFAULT '{}',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON bank_entity (cohort_id);

-- -------------------------------------------------------
-- bank_account
-- -------------------------------------------------------
CREATE TABLE bank_account (
  id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  cohort_id    UUID        NOT NULL REFERENCES cohort(id),
  entity_id    UUID        NOT NULL REFERENCES bank_entity(id),
  account_type TEXT        NOT NULL CHECK (account_type IN ('checking','savings','crypto')),
  currency     CHAR(3)     NOT NULL,
  opened_at    TIMESTAMPTZ NOT NULL,
  attributes   JSONB       NOT NULL DEFAULT '{}',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON bank_account (cohort_id);
CREATE INDEX ON bank_account (entity_id);

-- -------------------------------------------------------
-- bank_transaction
-- -------------------------------------------------------
CREATE TABLE bank_transaction (
  id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  cohort_id       UUID         NOT NULL REFERENCES cohort(id),
  from_account_id UUID         NOT NULL REFERENCES bank_account(id),
  to_account_id   UUID         NOT NULL REFERENCES bank_account(id),
  amount          NUMERIC(18,4) NOT NULL,
  currency        CHAR(3)      NOT NULL,
  tx_type         TEXT         NOT NULL CHECK (tx_type IN ('wire','ach','crypto','cash')),
  tx_timestamp    TIMESTAMPTZ  NOT NULL,
  reference       TEXT,
  attributes      JSONB        NOT NULL DEFAULT '{}',
  created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX ON bank_transaction (cohort_id, tx_timestamp);
CREATE INDEX ON bank_transaction (from_account_id);
CREATE INDEX ON bank_transaction (to_account_id);

-- -------------------------------------------------------
-- scenario_proposal
-- -------------------------------------------------------
CREATE TABLE scenario_proposal (
  id                       UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  cohort_id                UUID        NOT NULL REFERENCES cohort(id),
  status                   TEXT        NOT NULL DEFAULT 'pending'
                                       CHECK (status IN ('pending','approved','rejected')),
  scenario_type            TEXT        NOT NULL,
  description              TEXT        NOT NULL,
  expected_evasion_vector  TEXT        NOT NULL,
  suggested_tx_pattern     JSONB       NOT NULL DEFAULT '{}',
  gemini_prompt            TEXT        NOT NULL,
  gemini_response          TEXT        NOT NULL,
  gemini_model             TEXT        NOT NULL,
  correlation_id           UUID        NOT NULL,
  approved_by              TEXT,
  created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON scenario_proposal (cohort_id, status);
CREATE TRIGGER scenario_proposal_updated_at
  BEFORE UPDATE ON scenario_proposal
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- -------------------------------------------------------
-- experiment
-- -------------------------------------------------------
CREATE TABLE experiment (
  id                   UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
  cohort_id            UUID   NOT NULL REFERENCES cohort(id),
  scenario_proposal_id UUID   REFERENCES scenario_proposal(id),
  name                 TEXT   NOT NULL,
  status               TEXT   NOT NULL DEFAULT 'pending'
                              CHECK (status IN ('pending','running','completed','failed')),
  aml_rule_version     TEXT   NOT NULL,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER experiment_updated_at
  BEFORE UPDATE ON experiment
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- -------------------------------------------------------
-- experiment_run
-- -------------------------------------------------------
CREATE TABLE experiment_run (
  id            UUID   PRIMARY KEY DEFAULT gen_random_uuid(),
  experiment_id UUID   NOT NULL REFERENCES experiment(id),
  run_type      TEXT   NOT NULL CHECK (run_type IN ('baseline','variant')),
  mutation_spec JSONB,
  alert_count   INT    NOT NULL DEFAULT 0,
  alert_summary JSONB  NOT NULL DEFAULT '{}',
  raw_alerts    JSONB  NOT NULL DEFAULT '[]',
  started_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  completed_at  TIMESTAMPTZ
);
CREATE INDEX ON experiment_run (experiment_id, run_type);

-- -------------------------------------------------------
-- experiment_diff
-- -------------------------------------------------------
CREATE TABLE experiment_diff (
  id                    UUID  PRIMARY KEY DEFAULT gen_random_uuid(),
  experiment_id         UUID  NOT NULL REFERENCES experiment(id),
  baseline_run_id       UUID  NOT NULL REFERENCES experiment_run(id),
  variant_run_id        UUID  NOT NULL REFERENCES experiment_run(id),
  evasion_delta         INT   NOT NULL,
  new_alert_rule_ids    TEXT[] NOT NULL DEFAULT '{}',
  dropped_alert_rule_ids TEXT[] NOT NULL DEFAULT '{}',
  created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- -------------------------------------------------------
-- rule_coverage
-- -------------------------------------------------------
CREATE TABLE rule_coverage (
  id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
  rule_id          TEXT         NOT NULL UNIQUE,
  rule_version     TEXT         NOT NULL,
  times_triggered  INT          NOT NULL DEFAULT 0,
  last_triggered_at TIMESTAMPTZ,
  coverage_weight  NUMERIC(5,4) NOT NULL DEFAULT 1.0,
  updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX ON rule_coverage (coverage_weight DESC);
CREATE TRIGGER rule_coverage_updated_at
  BEFORE UPDATE ON rule_coverage
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- -------------------------------------------------------
-- evidence_bundle
-- -------------------------------------------------------
CREATE TABLE evidence_bundle (
  id            UUID  PRIMARY KEY DEFAULT gen_random_uuid(),
  experiment_id UUID  NOT NULL REFERENCES experiment(id),
  bundle        JSONB NOT NULL,
  checksum      TEXT  NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON evidence_bundle USING GIN (bundle);

-- -------------------------------------------------------
-- job (worker queue — DECISION-004 baseline option A)
-- -------------------------------------------------------
CREATE TABLE job (
  id           UUID  PRIMARY KEY DEFAULT gen_random_uuid(),
  job_type     TEXT  NOT NULL,
  payload      JSONB NOT NULL DEFAULT '{}',
  status       TEXT  NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending','running','completed','failed')),
  attempts     INT   NOT NULL DEFAULT 0,
  max_attempts INT   NOT NULL DEFAULT 5,
  last_error   TEXT,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON job (status, created_at) WHERE status IN ('pending','running');
CREATE TRIGGER job_updated_at
  BEFORE UPDATE ON job
  FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

COMMIT;
