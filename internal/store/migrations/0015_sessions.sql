-- +goose Up
ALTER TABLE runs
    ADD COLUMN session_id uuid,
    ADD COLUMN message text;
