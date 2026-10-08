-- +goose Up
ALTER TABLE users
    ADD COLUMN failed_count     int NOT NULL DEFAULT 0,
    ADD COLUMN next_attempt_at  timestamptz;
