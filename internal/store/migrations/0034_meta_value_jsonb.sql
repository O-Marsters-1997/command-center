-- +goose Up
SET LOCAL lock_timeout = '5s';
ALTER TABLE meta ALTER COLUMN value TYPE jsonb USING value::jsonb;
