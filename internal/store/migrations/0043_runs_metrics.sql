-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE runs ADD COLUMN metrics jsonb;

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE runs DROP COLUMN metrics;
