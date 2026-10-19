-- 000012_campaign.up.sql
--
-- Campaigns group counterfactual experiments under a shared goal, cohort and
-- budget. They are persisted so GET /campaigns/{id} and campaign coverage are
-- backed by real rows rather than a mock response.

CREATE TABLE IF NOT EXISTS campaign (
    id               UUID PRIMARY KEY,
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    goal             TEXT NOT NULL DEFAULT '',
    cohort_id        UUID REFERENCES cohort(id) ON DELETE SET NULL,
    budget           INTEGER NOT NULL CHECK (budget > 0),
    experiments_used INTEGER NOT NULL DEFAULT 0 CHECK (experiments_used >= 0),
    status           TEXT NOT NULL DEFAULT 'active'
                     CHECK (status IN ('active', 'completed', 'paused')),
    control_config   JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE experiment_pair
    ADD COLUMN IF NOT EXISTS campaign_id UUID REFERENCES campaign(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS experiment_pair_campaign_idx ON experiment_pair (campaign_id);
