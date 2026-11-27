-- +goose Up
SET lock_timeout = '5s';
UPDATE intents i
SET finished_at = r.ended_at,
    outcome = CASE WHEN r.outcome IN ('push', 'continued', 'passed') THEN 'done' ELSE 'failed' END
FROM runs r
WHERE i.run_id = r.id AND r.outcome IS NOT NULL AND r.ended_at IS NOT NULL AND i.finished_at IS NULL;

-- +goose Down
-- +goose StatementBegin
SELECT 'irreversible: closed intents cannot be told from ones still open'::int;
-- +goose StatementEnd
