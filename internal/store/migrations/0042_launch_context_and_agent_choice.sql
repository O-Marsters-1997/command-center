-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE launches
    ADD COLUMN context text NOT NULL DEFAULT '',
    ADD COLUMN model text,
    ADD COLUMN effort text CONSTRAINT launches_effort_check CHECK (effort IN ('low', 'medium', 'high'));
ALTER TABLE runs
    ADD COLUMN model text,
    ADD COLUMN effort text CONSTRAINT runs_effort_check CHECK (effort IN ('low', 'medium', 'high'));

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE runs DROP COLUMN effort, DROP COLUMN model;
ALTER TABLE launches DROP COLUMN effort, DROP COLUMN model, DROP COLUMN context;
