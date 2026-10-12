-- +goose Up
DROP TABLE run_requests;

ALTER TABLE runs
    DROP COLUMN tokens_in,
    DROP COLUMN tokens_out,
    DROP COLUMN turns,
    DROP COLUMN duration_ms,
    DROP COLUMN model,
    DROP COLUMN metrics_settled;

UPDATE launches SET state = 'cancelled' WHERE state = 'done';
ALTER TABLE launches DROP CONSTRAINT launches_state_check;
ALTER TABLE launches ADD CONSTRAINT launches_state_check CHECK (state IN ('active', 'cancelled'));
