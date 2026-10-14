-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE runs ADD CONSTRAINT runs_kind_check
    CHECK (kind IN ('agent', 'follow_up')) NOT VALID;
ALTER TABLE runs ADD CONSTRAINT runs_outcome_check
    CHECK (outcome IS NULL OR outcome IN ('push', 'failed', 'cut_failed', 'asked', 'continued')) NOT VALID;
ALTER TABLE intents ADD CONSTRAINT intents_verb_check
    CHECK (verb IN ('launch', 'kill', 're-run', 'retry-push', 'remove-worktree', 'cancel',
        'refresh', 'abort', 'follow-up', 'import', 'track', 'edit_ticket')) NOT VALID;
ALTER TABLE repos ADD CONSTRAINT repos_refusal_kind_check
    CHECK (refusal_kind IS NULL
        OR refusal_kind IN ('merge_settings', 'default_branch', 'clone', 'settings_parse')) NOT VALID;
ALTER TABLE events ADD CONSTRAINT events_kind_check
    CHECK (kind IN ('push_refused', 'push_failed', 'pushed', 'import_refused', 'remove_worktree_refused',
        'verdict_transition', 'tick_error', 'refresh_refused', 'refresh_conflicted', 'refreshed',
        'restacked', 'verification_failed', 'retargeted', 'retarget_failed', 'pr_opened',
        'pr_merged', 'checks_landed', 'run_launched', 'run_disposed', 'cut_failed',
        'spawn_failed', 'worktree_removed', 'launch_cancelled', 'merge_aborted',
        'merge_abort_failed', 'follow_up_refused', 'draft_ready', 'draft_ready_failed', 'launch')) NOT VALID;
ALTER TABLE utilization_readings ADD CONSTRAINT utilization_readings_window_check
    CHECK ("window" IN ('five_hour', 'seven_day')) NOT VALID;
ALTER TABLE utilization_intervals ADD CONSTRAINT utilization_intervals_window_check
    CHECK ("window" IN ('five_hour', 'seven_day')) NOT VALID;

-- +goose Down
SET lock_timeout = '5s';
ALTER TABLE utilization_intervals DROP CONSTRAINT utilization_intervals_window_check;
ALTER TABLE utilization_readings DROP CONSTRAINT utilization_readings_window_check;
ALTER TABLE events DROP CONSTRAINT events_kind_check;
ALTER TABLE repos DROP CONSTRAINT repos_refusal_kind_check;
ALTER TABLE intents DROP CONSTRAINT intents_verb_check;
ALTER TABLE runs DROP CONSTRAINT runs_outcome_check;
ALTER TABLE runs DROP CONSTRAINT runs_kind_check;
