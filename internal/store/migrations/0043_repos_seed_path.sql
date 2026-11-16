-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE repos ADD COLUMN seed_path text;

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE repos DROP COLUMN seed_path;
