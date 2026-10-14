-- +goose Up
SET LOCAL lock_timeout = '5s';
ALTER TABLE intents ALTER COLUMN payload TYPE jsonb
    USING CASE WHEN verb = 'follow_up' THEN to_jsonb(payload) ELSE payload::jsonb END;
