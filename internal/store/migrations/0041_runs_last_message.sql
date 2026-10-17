-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE runs ADD COLUMN last_message text;

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE runs DROP COLUMN last_message;
