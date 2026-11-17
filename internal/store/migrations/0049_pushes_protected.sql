-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE pushes ADD COLUMN protected jsonb NOT NULL DEFAULT '[]';

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE pushes DROP COLUMN protected;
