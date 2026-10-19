DROP TABLE IF EXISTS scenario_coverage;
DROP TABLE IF EXISTS outcome_coverage;
DROP TABLE IF EXISTS rule_outcome;

ALTER TABLE rule_coverage
    DROP COLUMN IF EXISTS times_not_triggered,
    DROP COLUMN IF EXISTS times_not_applicable,
    DROP COLUMN IF EXISTS times_inconclusive,
    DROP COLUMN IF EXISTS times_failed,
    DROP COLUMN IF EXISTS last_status,
    DROP COLUMN IF EXISTS last_evaluated_at;
