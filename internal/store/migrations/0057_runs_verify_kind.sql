-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE runs DROP CONSTRAINT runs_kind_check;
ALTER TABLE runs ADD CONSTRAINT runs_kind_check
    CHECK (kind IN ('agent', 'follow_up', 'command', 'resolve', 'verify')) NOT VALID;
ALTER TABLE runs VALIDATE CONSTRAINT runs_kind_check;

-- +goose Down
SET lock_timeout = '5s';
DELETE FROM runs WHERE kind = 'verify';
ALTER TABLE runs DROP CONSTRAINT runs_kind_check;
ALTER TABLE runs ADD CONSTRAINT runs_kind_check
    CHECK (kind IN ('agent', 'follow_up', 'command', 'resolve')) NOT VALID;
