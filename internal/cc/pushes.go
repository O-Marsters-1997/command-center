package cc

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RecordPush writes one successful push -- the row Phase 4's crash-safety hinges on:
// pushed_tip is compared against on every later tick's plan.PushPlan, so a duplicate push or a
// duplicate PR create both stop the moment this lands (inv. 20).
func (s *Store) RecordPush(ctx context.Context, taskID, pushedTip, baseBranch, baseSHA string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pushes (task_id, pushed_tip, base_branch, base_sha_at_push, pushed_at)
		VALUES (?, ?, ?, ?, ?)`,
		taskID, pushedTip, baseBranch, baseSHA, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("record push for %s: %w", taskID, err)
	}
	return s.resetCheckingTicks(ctx, taskID)
}

// LastPushedTips returns each task's most recently recorded pushed_tip -- what plan.PushPlan
// compares a branch's current local tip against.
func (s *Store) LastPushedTips(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.task_id, p.pushed_tip FROM pushes p
		JOIN (SELECT task_id, MAX(id) AS id FROM pushes GROUP BY task_id) latest
		  ON latest.task_id = p.task_id AND latest.id = p.id`)
	if err != nil {
		return nil, fmt.Errorf("select last pushed tips: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tips := map[string]string{}
	for rows.Next() {
		var taskID, tip string
		if err := rows.Scan(&taskID, &tip); err != nil {
			return nil, fmt.Errorf("scan pushed tip: %w", err)
		}
		tips[taskID] = tip
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pushed tips: %w", err)
	}
	return tips, nil
}

// PushRow is one task's latest recorded push, in full -- what internal/verdict's Input needs
// beyond the check rollup itself: which tip and base it was pushed against, and when.
type PushRow struct {
	PushedTip     string
	BaseBranch    string
	BaseSHAAtPush string
	PushedAt      time.Time
}

// LatestPushes returns each task's latest recorded push in full, keyed by ticket URL -- the CI
// verdict step's own per-task facts, read fresh every render (inv. 14).
func (s *Store) LatestPushes(ctx context.Context) (map[string]PushRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.task_id, p.pushed_tip, p.base_branch, p.base_sha_at_push, p.pushed_at FROM pushes p
		JOIN (SELECT task_id, MAX(id) AS id FROM pushes GROUP BY task_id) latest
		  ON latest.task_id = p.task_id AND latest.id = p.id`)
	if err != nil {
		return nil, fmt.Errorf("select latest pushes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	pushes := map[string]PushRow{}
	for rows.Next() {
		var taskID, pushedAt string
		var row PushRow
		if err := rows.Scan(&taskID, &row.PushedTip, &row.BaseBranch, &row.BaseSHAAtPush, &pushedAt); err != nil {
			return nil, fmt.Errorf("scan push row: %w", err)
		}
		if row.PushedAt, err = time.Parse(time.RFC3339Nano, pushedAt); err != nil {
			return nil, fmt.Errorf("decode pushed_at %q: %w", pushedAt, err)
		}
		pushes[taskID] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate push rows: %w", err)
	}
	return pushes, nil
}

// PushFact is a task's outstanding push-policy problem: refused outright (naming the path), or
// a push/PR-create failure. Neither is a stored column (inv. 14) -- both are derived from the
// latest push_refused/push_failed event since the task's last recorded push, so a later success clears it.
type PushFact struct {
	Refused     bool
	RefusedPath string
	Failed      bool
}

const (
	eventPushRefused = "push_refused"
	eventPushFailed  = "push_failed"
	eventPushed      = "pushed"
)

// PushFacts returns every task's outstanding push-policy problem, keyed by ticket URL: what the
// automatic push step's auto-retry gate (a failure, never a refusal, blocks it -- retry-push is
// your verb) and the page's needs-you/push-failed rendering both read.
func (s *Store) PushFacts(ctx context.Context) (map[string]PushFact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.task_id, e.kind, e.detail
		FROM events e
		JOIN (
			SELECT e2.task_id, MAX(e2.id) AS id
			FROM events e2
			LEFT JOIN (
				SELECT task_id, MAX(pushed_at) AS pushed_at FROM pushes GROUP BY task_id
			) p ON p.task_id = e2.task_id
			WHERE e2.kind IN (?, ?) AND e2.at > COALESCE(p.pushed_at, '')
			GROUP BY e2.task_id
		) latest ON latest.task_id = e.task_id AND latest.id = e.id`,
		eventPushRefused, eventPushFailed)
	if err != nil {
		return nil, fmt.Errorf("select push facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	facts := map[string]PushFact{}
	for rows.Next() {
		var taskID, kind string
		var detail sql.NullString
		if err := rows.Scan(&taskID, &kind, &detail); err != nil {
			return nil, fmt.Errorf("scan push fact: %w", err)
		}
		switch kind {
		case eventPushRefused:
			facts[taskID] = PushFact{Refused: true, RefusedPath: detail.String}
		case eventPushFailed:
			facts[taskID] = PushFact{Failed: true}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate push facts: %w", err)
	}
	return facts, nil
}
