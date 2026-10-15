-- 000001_init.down.sql
-- Reverses the initial schema migration.
-- WARNING: This drops all data. Only use in development.

BEGIN;

DROP TABLE IF EXISTS evidence_bundle;
DROP TABLE IF EXISTS job;
DROP TABLE IF EXISTS rule_coverage;
DROP TABLE IF EXISTS experiment_diff;
DROP TABLE IF EXISTS experiment_run;
DROP TABLE IF EXISTS experiment;
DROP TABLE IF EXISTS scenario_proposal;
DROP TABLE IF EXISTS bank_transaction;
DROP TABLE IF EXISTS bank_account;
DROP TABLE IF EXISTS bank_entity;
DROP TABLE IF EXISTS cohort;

DROP FUNCTION IF EXISTS trigger_set_updated_at();
DROP EXTENSION IF EXISTS "pgcrypto";

COMMIT;
