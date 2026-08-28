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
func (s *Store) RecordPush(ctx context.Context, ticketID, pushedTip, baseBranch, baseSHA string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO pushes (ticket_id, pushed_tip, base_branch, base_sha_at_push, pushed_at)
		VALUES (?, ?, ?, ?, ?)`,
		ticketID, pushedTip, baseBranch, baseSHA, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("record push for %s: %w", ticketID, err)
	}
	return s.resetCheckingTicks(ctx, ticketID)
}

// RestackedSinceLastPush names every ticket whose branch the app itself rebased since it last
// recorded a push of it, which is the only licence the push step has to lease-force (issue #89).
// The comparison is >= rather than >: retargetOne stamps its push row and the restack that
// follows it with one tick's single clock reading, so a strict > would never see its own work.
func (s *Store) RestackedSinceLastPush(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT e.ticket_id
		FROM events e
		LEFT JOIN (
			SELECT ticket_id, MAX(pushed_at) AS pushed_at FROM pushes GROUP BY ticket_id
		) p ON p.ticket_id = e.ticket_id
		WHERE e.kind = ? AND e.at >= COALESCE(p.pushed_at, '')`, eventRestacked)
	if err != nil {
		return nil, fmt.Errorf("select restacked tickets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	restacked := map[string]bool{}
	for rows.Next() {
		var ticketID string
		if err := rows.Scan(&ticketID); err != nil {
			return nil, fmt.Errorf("scan restacked ticket: %w", err)
		}
		restacked[ticketID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate restacked tickets: %w", err)
	}
	return restacked, nil
}

// LastPushedTips returns each ticket's most recently recorded pushed_tip -- what plan.PushPlan
// compares a branch's current local tip against.
func (s *Store) LastPushedTips(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.ticket_id, p.pushed_tip FROM pushes p
		JOIN (SELECT ticket_id, MAX(id) AS id FROM pushes GROUP BY ticket_id) latest
		  ON latest.ticket_id = p.ticket_id AND latest.id = p.id`)
	if err != nil {
		return nil, fmt.Errorf("select last pushed tips: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tips := map[string]string{}
	for rows.Next() {
		var ticketID, tip string
		if err := rows.Scan(&ticketID, &tip); err != nil {
			return nil, fmt.Errorf("scan pushed tip: %w", err)
		}
		tips[ticketID] = tip
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pushed tips: %w", err)
	}
	return tips, nil
}

// PushRow is one ticket's latest recorded push, in full -- what internal/verdict's Input needs
// beyond the check rollup itself: which tip and base it was pushed against, and when.
type PushRow struct {
	PushedTip     string
	BaseBranch    string
	BaseSHAAtPush string
	PushedAt      time.Time
}

// LatestPushes returns each ticket's latest recorded push in full, keyed by ticket URL -- the CI
// verdict step's own per-ticket facts, read fresh every render (inv. 14).
func (s *Store) LatestPushes(ctx context.Context) (map[string]PushRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.ticket_id, p.pushed_tip, p.base_branch, p.base_sha_at_push, p.pushed_at FROM pushes p
		JOIN (SELECT ticket_id, MAX(id) AS id FROM pushes GROUP BY ticket_id) latest
		  ON latest.ticket_id = p.ticket_id AND latest.id = p.id`)
	if err != nil {
		return nil, fmt.Errorf("select latest pushes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	pushes := map[string]PushRow{}
	for rows.Next() {
		var ticketID, pushedAt string
		var row PushRow
		if err := rows.Scan(&ticketID, &row.PushedTip, &row.BaseBranch, &row.BaseSHAAtPush, &pushedAt); err != nil {
			return nil, fmt.Errorf("scan push row: %w", err)
		}
		if row.PushedAt, err = time.Parse(time.RFC3339Nano, pushedAt); err != nil {
			return nil, fmt.Errorf("decode pushed_at %q: %w", pushedAt, err)
		}
		pushes[ticketID] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate push rows: %w", err)
	}
	return pushes, nil
}

// PushFact is a ticket's outstanding push-policy problem: refused outright (naming the path), or
// a push/PR-create failure. Neither is a stored column (inv. 14) -- both are derived from the
// latest push_refused/push_failed event since the ticket's last recorded push, so a later success clears it.
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

// PushFacts returns every ticket's outstanding push-policy problem, keyed by ticket URL: what the
// automatic push step's auto-retry gate (a failure, never a refusal, blocks it -- retry-push is
// your verb) and the page's needs-you/push-failed rendering both read.
func (s *Store) PushFacts(ctx context.Context) (map[string]PushFact, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.ticket_id, e.kind, e.detail
		FROM events e
		JOIN (
			SELECT e2.ticket_id, MAX(e2.id) AS id
			FROM events e2
			LEFT JOIN (
				SELECT ticket_id, MAX(pushed_at) AS pushed_at FROM pushes GROUP BY ticket_id
			) p ON p.ticket_id = e2.ticket_id
			WHERE e2.kind IN (?, ?) AND e2.at > COALESCE(p.pushed_at, '')
			GROUP BY e2.ticket_id
		) latest ON latest.ticket_id = e.ticket_id AND latest.id = e.id`,
		eventPushRefused, eventPushFailed)
	if err != nil {
		return nil, fmt.Errorf("select push facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	facts := map[string]PushFact{}
	for rows.Next() {
		var ticketID, kind string
		var detail sql.NullString
		if err := rows.Scan(&ticketID, &kind, &detail); err != nil {
			return nil, fmt.Errorf("scan push fact: %w", err)
		}
		switch kind {
		case eventPushRefused:
			facts[ticketID] = PushFact{Refused: true, RefusedPath: detail.String}
		case eventPushFailed:
			facts[ticketID] = PushFact{Failed: true}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate push facts: %w", err)
	}
	return facts, nil
}
