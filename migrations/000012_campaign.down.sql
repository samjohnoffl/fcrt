DROP INDEX IF EXISTS experiment_pair_campaign_idx;
ALTER TABLE experiment_pair DROP COLUMN IF EXISTS campaign_id;
DROP TABLE IF EXISTS campaign;
