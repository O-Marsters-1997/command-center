-- +goose Up
ALTER TABLE tasks RENAME TO tickets;
ALTER TABLE tickets RENAME COLUMN ticket_url TO url;
ALTER TABLE runs RENAME COLUMN task_id TO ticket_id;
ALTER TABLE pushes RENAME COLUMN task_id TO ticket_id;
ALTER TABLE launch_members RENAME COLUMN task_id TO ticket_id;
ALTER TABLE events RENAME COLUMN task_id TO ticket_id;
ALTER TABLE intents RENAME COLUMN task_id TO ticket_id;
