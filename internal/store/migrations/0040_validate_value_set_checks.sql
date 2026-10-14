-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE runs VALIDATE CONSTRAINT runs_kind_check;
ALTER TABLE runs VALIDATE CONSTRAINT runs_outcome_check;
ALTER TABLE intents VALIDATE CONSTRAINT intents_verb_check;
ALTER TABLE repos VALIDATE CONSTRAINT repos_refusal_kind_check;
ALTER TABLE events VALIDATE CONSTRAINT events_kind_check;
ALTER TABLE utilization_readings VALIDATE CONSTRAINT utilization_readings_window_check;
ALTER TABLE utilization_intervals VALIDATE CONSTRAINT utilization_intervals_window_check;

-- +goose Down
-- +goose StatementBegin
SELECT 'irreversible: validated constraints cannot be marked NOT VALID'::int;
-- +goose StatementEnd
