-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE runs
    ADD COLUMN step text NOT NULL DEFAULT 'implement',
    ADD COLUMN workflow text NOT NULL DEFAULT 'v0',
    ADD COLUMN workflow_hash text NOT NULL DEFAULT '92dbae79cf537fa1b42a130218fa6271fc7d078f520b3102df10f6b267530f7e';
ALTER TABLE runs
    ALTER COLUMN step DROP DEFAULT,
    ALTER COLUMN workflow DROP DEFAULT,
    ALTER COLUMN workflow_hash DROP DEFAULT;

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE runs DROP COLUMN workflow_hash, DROP COLUMN workflow, DROP COLUMN step;
