package cc

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// InsertRunSkeleton reserves a runs row before the process exists: task_id, kind, baseline_sha
// and prompt_hash are known at cut time, but pgid and log_path are named after the row's own id
// (docs/prds/prd-command-centre.md § A run), so they land in a later RecordSpawn.
func (s *Store) InsertRunSkeleton(ctx context.Context, taskID, kind, baselineSHA, promptHash string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (task_id, kind, baseline_sha, prompt_hash) VALUES (?, ?, ?, ?)`,
		taskID, kind, baselineSHA, promptHash)
	if err != nil {
		return 0, fmt.Errorf("insert run skeleton for %s: %w", taskID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("run id for %s: %w", taskID, err)
	}
	return id, nil
}

// RecordSpawn writes the pgid, its process start time and the log path onto a reserved run row,
// in the one UPDATE that must happen immediately after Spawn returns and nothing else.
func (s *Store) RecordSpawn(ctx context.Context, runID int64, pgid int, startedAt time.Time, logPath string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET pgid = ?, proc_started_at = ?, log_path = ? WHERE id = ?`,
		pgid, startedAt.UTC().Format(time.RFC3339Nano), logPath, runID)
	if err != nil {
		return fmt.Errorf("record spawn for run %d: %w", runID, err)
	}
	return nil
}

// RecordDisposition writes a dead run's outcome. exitCode is nil when there is none to report —
// a spawn that never started a process, or one this instance could not reap.
func (s *Store) RecordDisposition(
	ctx context.Context, runID int64, outcome plan.Outcome, exitCode *int, endedAt time.Time,
) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE runs SET outcome = ?, exit_code = ?, ended_at = ? WHERE id = ?`,
		outcome.String(), exitCode, endedAt.UTC().Format(time.RFC3339Nano), runID)
	if err != nil {
		return fmt.Errorf("record disposition for run %d: %w", runID, err)
	}
	return nil
}

// InsertCutFailedRun records a run that never got a worktree, in one INSERT: no baseline, no
// pgid, ever (docs/prds/prd-command-centre.md § The states, cut failed).
func (s *Store) InsertCutFailedRun(ctx context.Context, taskID, promptHash string, at time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO runs (task_id, kind, prompt_hash, outcome, ended_at) VALUES (?, 'agent', ?, ?, ?)`,
		taskID, promptHash, plan.OutcomeCutFailed.String(), at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("insert cut-failed run for %s: %w", taskID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("cut-failed run id for %s: %w", taskID, err)
	}
	return id, nil
}

// PendingRun is one run this tick must check for liveness and, if it has died, dispose of.
type PendingRun struct {
	ID            int64
	TaskID        string
	Pgid          int
	ProcStartedAt time.Time
	BaselineSHA   string
	LogPath       string
}

// PendingRunsAwaitingDisposition returns every run with a pgid but no outcome yet: the exact
// set the loop's liveness+disposition pass must check, every tick, restart or not.
func (s *Store) PendingRunsAwaitingDisposition(ctx context.Context) ([]PendingRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, pgid, proc_started_at, baseline_sha, log_path FROM runs
		WHERE pgid IS NOT NULL AND outcome IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("select pending runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pending []PendingRun
	for rows.Next() {
		var p PendingRun
		var startedAt string
		var baselineSHA, logPath sql.NullString
		if err := rows.Scan(&p.ID, &p.TaskID, &p.Pgid, &startedAt, &baselineSHA, &logPath); err != nil {
			return nil, fmt.Errorf("scan pending run: %w", err)
		}
		if p.ProcStartedAt, err = time.Parse(time.RFC3339Nano, startedAt); err != nil {
			return nil, fmt.Errorf("decode proc_started_at %q: %w", startedAt, err)
		}
		p.BaselineSHA, p.LogPath = baselineSHA.String, logPath.String
		pending = append(pending, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending runs: %w", err)
	}
	return pending, nil
}

// RunSummary is the latest run the page and the launch-eligibility check need per task.
type RunSummary struct {
	ID            int64
	Pgid          *int
	ProcStartedAt *time.Time
	HasOutcome    bool
	Outcome       plan.Outcome
	ExitCode      *int
	EndedAt       *time.Time
	LogPath       string
	BaselineSHA   string
	// PromptHash is what this run was spawned (or cut-failed) against — the page's own seam-
	// changed comparison for a row that has already run compares a fresh recomposition against
	// this, never the launch membership's hash, which a later re-run or relaunch supersedes.
	PromptHash string
}

// LatestRunsByTask returns each task's single most recent run (highest id). Its presence alone
// is what LaunchPlan's "no prior run" rule and the page's pgid/elapsed/log-path columns need.
func (s *Store) LatestRunsByTask(ctx context.Context) (map[string]RunSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.task_id, r.pgid, r.proc_started_at, r.baseline_sha, r.log_path,
		       r.outcome, r.exit_code, r.ended_at, r.prompt_hash
		FROM runs r
		JOIN (SELECT task_id, MAX(id) AS id FROM runs GROUP BY task_id) latest
		  ON latest.task_id = r.task_id AND latest.id = r.id`)
	if err != nil {
		return nil, fmt.Errorf("select latest runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	summaries := map[string]RunSummary{}
	for rows.Next() {
		var taskID string
		var summary RunSummary
		var pgid sql.NullInt64
		var procStartedAt, baselineSHA, logPath, outcome, endedAt, promptHash sql.NullString
		var exitCode sql.NullInt64
		if err := rows.Scan(&summary.ID, &taskID, &pgid, &procStartedAt, &baselineSHA, &logPath,
			&outcome, &exitCode, &endedAt, &promptHash); err != nil {
			return nil, fmt.Errorf("scan latest run: %w", err)
		}
		if pgid.Valid {
			v := int(pgid.Int64)
			summary.Pgid = &v
		}
		if procStartedAt.Valid {
			t, err := time.Parse(time.RFC3339Nano, procStartedAt.String)
			if err != nil {
				return nil, fmt.Errorf("decode proc_started_at %q: %w", procStartedAt.String, err)
			}
			summary.ProcStartedAt = &t
		}
		if endedAt.Valid {
			t, err := time.Parse(time.RFC3339Nano, endedAt.String)
			if err != nil {
				return nil, fmt.Errorf("decode ended_at %q: %w", endedAt.String, err)
			}
			summary.EndedAt = &t
		}
		if exitCode.Valid {
			v := int(exitCode.Int64)
			summary.ExitCode = &v
		}
		if outcome.Valid {
			summary.HasOutcome = true
			summary.Outcome = outcomeFromString(outcome.String)
		}
		summary.BaselineSHA, summary.LogPath = baselineSHA.String, logPath.String
		summary.PromptHash = promptHash.String
		summaries[taskID] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest runs: %w", err)
	}
	return summaries, nil
}

func outcomeFromString(s string) plan.Outcome {
	switch s {
	case plan.OutcomePush.String():
		return plan.OutcomePush
	case plan.OutcomeCutFailed.String():
		return plan.OutcomeCutFailed
	default:
		return plan.OutcomeFailed
	}
}

// VerbIntent is one queued action against a task — the loop's own read-then-act sequence, never
// a handler's (see server.go's handleVerb).
type VerbIntent struct {
	ID     int64
	TaskID string
}

// QueueVerbIntent records one requested verb against a task. A handler only ever does this one
// blind INSERT; the loop is the sole reader and actor (inv. 9).
func (s *Store) QueueVerbIntent(ctx context.Context, taskID, verb string, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO intents (at, task_id, verb) VALUES (?, ?, ?)`,
		at.UTC().Format(time.RFC3339Nano), taskID, verb)
	if err != nil {
		return fmt.Errorf("queue %s intent for %s: %w", verb, taskID, err)
	}
	return nil
}

// PendingVerbIntents returns every unconsumed intent for verb, oldest first.
func (s *Store) PendingVerbIntents(ctx context.Context, verb string) ([]VerbIntent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id FROM intents WHERE verb = ? AND consumed_at IS NULL ORDER BY id`, verb)
	if err != nil {
		return nil, fmt.Errorf("select %s intents: %w", verb, err)
	}
	defer func() { _ = rows.Close() }()

	var intents []VerbIntent
	for rows.Next() {
		var v VerbIntent
		if err := rows.Scan(&v.ID, &v.TaskID); err != nil {
			return nil, fmt.Errorf("scan %s intent: %w", verb, err)
		}
		intents = append(intents, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s intents: %w", verb, err)
	}
	return intents, nil
}

// PendingIntentsByTask is every unconsumed intent, keyed by task, most recent last.
func (s *Store) PendingIntentsByTask(ctx context.Context) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, verb FROM intents WHERE consumed_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("select pending intents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	byTask := map[string][]string{}
	for rows.Next() {
		var taskID, verb string
		if err := rows.Scan(&taskID, &verb); err != nil {
			return nil, fmt.Errorf("scan pending intent: %w", err)
		}
		byTask[taskID] = append(byTask[taskID], verb)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending intents: %w", err)
	}
	return byTask, nil
}

// ConsumeVerbIntent marks one intent consumed, so a later tick never applies it again.
func (s *Store) ConsumeVerbIntent(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE intents SET consumed_at = ? WHERE id = ?`, at.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("consume intent %d: %w", id, err)
	}
	return nil
}

// ActiveLaunchHashes returns the authorised prompt hash per task, for every task in an active
// launch — what the loop's launch-eligibility check compares the recomposed hash against.
func (s *Store) ActiveLaunchHashes(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT lm.task_id, lm.prompt_hash FROM launch_members lm
		JOIN launches l ON l.id = lm.launch_id
		WHERE l.state = 'active'`)
	if err != nil {
		return nil, fmt.Errorf("select active launch hashes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	hashes := map[string]string{}
	for rows.Next() {
		var taskID, hash string
		if err := rows.Scan(&taskID, &hash); err != nil {
			return nil, fmt.Errorf("scan active launch hash: %w", err)
		}
		hashes[taskID] = hash
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active launch hashes: %w", err)
	}
	return hashes, nil
}

// RunIDsForTask returns every run id ever recorded for a task, oldest first -- what
// remove-worktree's log pruning needs to find every runs/<id>.jsonl, runs/<id>.prompt and
// runs/<id>.diff it left behind (docs/prds/prd-command-centre.md § Phase 6).
func (s *Store) RunIDsForTask(ctx context.Context, taskID string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM runs WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("select run ids for %s: %w", taskID, err)
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan run id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run ids for %s: %w", taskID, err)
	}
	return ids, nil
}
