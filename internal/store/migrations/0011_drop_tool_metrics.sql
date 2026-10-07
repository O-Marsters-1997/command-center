-- +goose Up
ALTER TABLE runs
    DROP COLUMN tool_calls,
    DROP COLUMN tool_failures;

ALTER TABLE run_requests DROP COLUMN tool;
