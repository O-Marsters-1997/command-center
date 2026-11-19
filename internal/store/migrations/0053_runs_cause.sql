-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE runs ADD COLUMN cause text;
ALTER TABLE runs ADD CONSTRAINT runs_cause_check
    CHECK (cause IS NULL OR cause IN ('ci_failed', 'verify_failed')) NOT VALID;
ALTER TABLE runs VALIDATE CONSTRAINT runs_cause_check;

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE runs DROP COLUMN cause;
