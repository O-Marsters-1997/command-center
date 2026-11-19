-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE intents ADD COLUMN hold_until timestamptz;

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE intents DROP COLUMN hold_until;
