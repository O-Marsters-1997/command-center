-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE launches ADD COLUMN workflow text NOT NULL DEFAULT 'v0';

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE launches DROP COLUMN workflow;
