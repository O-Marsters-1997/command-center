-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE runs DROP CONSTRAINT runs_kind_check, DROP CONSTRAINT runs_outcome_check;
ALTER TABLE runs ADD CONSTRAINT runs_kind_check
    CHECK (kind IN ('agent', 'follow_up', 'command')) NOT VALID;
ALTER TABLE runs ADD CONSTRAINT runs_outcome_check
    CHECK (outcome IS NULL OR outcome IN ('push', 'failed', 'cut_failed', 'asked', 'continued', 'passed')) NOT VALID;
ALTER TABLE runs VALIDATE CONSTRAINT runs_kind_check;
ALTER TABLE runs VALIDATE CONSTRAINT runs_outcome_check;

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE runs DROP CONSTRAINT runs_kind_check, DROP CONSTRAINT runs_outcome_check;
ALTER TABLE runs ADD CONSTRAINT runs_kind_check
    CHECK (kind IN ('agent', 'follow_up')) NOT VALID;
ALTER TABLE runs ADD CONSTRAINT runs_outcome_check
    CHECK (outcome IS NULL OR outcome IN ('push', 'failed', 'cut_failed', 'asked', 'continued')) NOT VALID;
