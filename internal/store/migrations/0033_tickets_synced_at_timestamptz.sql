-- +goose Up
SET LOCAL lock_timeout = '5s';
ALTER TABLE tickets ALTER COLUMN synced_at DROP DEFAULT;
ALTER TABLE tickets ALTER COLUMN synced_at DROP NOT NULL;
ALTER TABLE tickets ALTER COLUMN synced_at TYPE timestamptz USING NULLIF(synced_at, '')::timestamptz;
