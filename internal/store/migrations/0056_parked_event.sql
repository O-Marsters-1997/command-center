-- +goose Up
SET lock_timeout = '5s';
ALTER TABLE events DROP CONSTRAINT events_kind_check;
ALTER TABLE events ADD CONSTRAINT events_kind_check
    CHECK (kind IN ('push_refused', 'push_failed', 'pushed', 'import_refused', 'remove_worktree_refused',
        'verdict_transition', 'tick_error', 'refresh_refused', 'refresh_conflicted', 'refreshed',
        'restacked', 'verification_failed', 'retargeted', 'retarget_failed', 'pr_opened',
        'pr_merged', 'checks_landed', 'run_launched', 'run_disposed', 'cut_failed',
        'spawn_failed', 'worktree_removed', 'launch_cancelled', 'merge_aborted',
        'merge_abort_failed', 'follow_up_refused', 'draft_ready', 'draft_ready_failed', 'launch',
        'resolve_started', 'resolve_refused', 'ci_log_unavailable', 'raise_refused', 'parked')) NOT VALID;
ALTER TABLE events VALIDATE CONSTRAINT events_kind_check;

-- +goose Down
SET lock_timeout = '5s';
DELETE FROM events WHERE kind = 'parked';
ALTER TABLE events DROP CONSTRAINT events_kind_check;
ALTER TABLE events ADD CONSTRAINT events_kind_check
    CHECK (kind IN ('push_refused', 'push_failed', 'pushed', 'import_refused', 'remove_worktree_refused',
        'verdict_transition', 'tick_error', 'refresh_refused', 'refresh_conflicted', 'refreshed',
        'restacked', 'verification_failed', 'retargeted', 'retarget_failed', 'pr_opened',
        'pr_merged', 'checks_landed', 'run_launched', 'run_disposed', 'cut_failed',
        'spawn_failed', 'worktree_removed', 'launch_cancelled', 'merge_aborted',
        'merge_abort_failed', 'follow_up_refused', 'draft_ready', 'draft_ready_failed', 'launch',
        'resolve_started', 'resolve_refused', 'ci_log_unavailable', 'raise_refused')) NOT VALID;
ALTER TABLE events VALIDATE CONSTRAINT events_kind_check;
