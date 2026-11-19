-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE runs ADD COLUMN prompt text;

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE runs DROP COLUMN prompt;
