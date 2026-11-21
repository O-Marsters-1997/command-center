-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE intents
    ADD COLUMN queued_by text NOT NULL DEFAULT 'owner',
    ADD COLUMN outcome text,
    ADD COLUMN detail text,
    ADD COLUMN finished_at timestamptz,
    ADD COLUMN run_id bigint REFERENCES runs (id) ON DELETE SET NULL,
    ADD COLUMN ticket_state text,
    ADD CONSTRAINT intents_queued_by_check CHECK (queued_by IN ('owner', 'loop')),
    ADD CONSTRAINT intents_outcome_check CHECK (outcome IN ('done', 'failed', 'withdrawn')),
    ADD CONSTRAINT intents_outcome_finished_check CHECK ((outcome IS NULL) = (finished_at IS NULL));

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE intents
    DROP COLUMN queued_by,
    DROP COLUMN outcome,
    DROP COLUMN detail,
    DROP COLUMN finished_at,
    DROP COLUMN run_id,
    DROP COLUMN ticket_state;
