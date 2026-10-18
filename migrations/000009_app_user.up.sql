-- 000009_app_user.up.sql
--
-- Application users for AUTH_MODE=local. Replaces the environment-only user
-- list with a real, manageable store so admins can add/disable/remove users.
--
-- Password hashes are bcrypt (never plaintext). org_id follows the DECISION-003
-- convention and defaults to the single 'default' organisation (see 000006).
BEGIN;

CREATE TABLE IF NOT EXISTS app_user (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID        NOT NULL,
    username      TEXT        NOT NULL,
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL CHECK (role IN ('analyst', 'admin')),
    disabled      BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT app_user_username_unique UNIQUE (org_id, username)
);

CREATE INDEX IF NOT EXISTS idx_app_user_username ON app_user (username);

-- Attach the default organisation as the column DEFAULT so inserts that omit
-- org_id land in it (same mechanism as migration 000006).
DO $$
DECLARE
  def_org UUID;
BEGIN
  SELECT id INTO def_org FROM organisations WHERE name = 'default' ORDER BY created_at LIMIT 1;
  IF def_org IS NULL THEN
    INSERT INTO organisations (name) VALUES ('default') RETURNING id INTO def_org;
  END IF;
  EXECUTE format('ALTER TABLE app_user ALTER COLUMN org_id SET DEFAULT %L', def_org);
END $$;

COMMIT;
