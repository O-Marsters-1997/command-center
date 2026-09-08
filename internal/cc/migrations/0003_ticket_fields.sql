-- +goose Up
ALTER TABLE tickets ADD COLUMN source TEXT;
ALTER TABLE tickets ADD COLUMN title TEXT;
ALTER TABLE tickets ADD COLUMN body TEXT;
ALTER TABLE tickets ADD COLUMN status TEXT;
ALTER TABLE tickets ADD COLUMN group_key TEXT;
ALTER TABLE tickets ADD COLUMN synced_at TEXT;
