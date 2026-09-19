-- +goose Up
ALTER TABLE tickets ADD COLUMN withdrawn_at timestamptz;
