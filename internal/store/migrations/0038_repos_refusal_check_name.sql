-- +goose Up
SET LOCAL lock_timeout = '5s';
ALTER TABLE repos RENAME CONSTRAINT repos_check TO repos_refusal_check;
