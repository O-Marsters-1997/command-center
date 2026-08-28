package cc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// dbTx is the subset of *sql.Tx the helpers below need, so they take an interface rather than
// a concrete transaction type.
type dbTx interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// launchPayload is an intent's free-form payload for verb "launch". group ties every intent
// from one POST /launch call together, without a batch-key column: the frozen v1 schema's
// intents.payload is free-form JSON precisely so this needs no migration.
type launchPayload struct {
	PromptHash string `json:"prompt_hash"`
	Group      string `json:"group"`
}

// QueueLaunchIntent records one ticket's authorisation to launch. The next tick's
// ApplyLaunchIntents turns every intent sharing a group into one launches row.
func (s *Store) QueueLaunchIntent(ctx context.Context, ticketID, promptHash, group string, at time.Time) error {
	payload, err := json.Marshal(launchPayload{PromptHash: promptHash, Group: group})
	if err != nil {
		return fmt.Errorf("encode launch intent payload for %s: %w", ticketID, err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO intents (at, ticket_id, verb, payload) VALUES (?, ?, 'launch', ?)`,
		at.UTC().Format(time.RFC3339Nano), ticketID, string(payload))
	if err != nil {
		return fmt.Errorf("queue launch intent for %s: %w", ticketID, err)
	}
	return nil
}

// ApplyLaunchIntents turns every unconsumed launch intent into a launch: one launches row per
// group plus one launch_members row per intent. Called once per tick; every intent it touches
// it also marks consumed, so re-applying with nothing new queued is a no-op.
func (s *Store) ApplyLaunchIntents(ctx context.Context, now time.Time) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	groups, order, err := pendingLaunchIntents(ctx, tx)
	if err != nil {
		return err
	}

	nowStr := now.UTC().Format(time.RFC3339Nano)
	for _, group := range order {
		if err := insertLaunch(ctx, tx, nowStr, groups[group]); err != nil {
			return fmt.Errorf("apply launch group %s: %w", group, err)
		}
	}
	return tx.Commit()
}

type pendingIntent struct {
	id         int64
	ticketID   string
	promptHash string
}

// pendingLaunchIntents reads every unconsumed launch intent and groups it by its payload's
// group field. order preserves first-seen group order, so launches are created deterministically.
func pendingLaunchIntents(ctx context.Context, tx dbTx) (map[string][]pendingIntent, []string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, ticket_id, payload FROM intents WHERE verb = 'launch' AND consumed_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, nil, fmt.Errorf("select launch intents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	groups := map[string][]pendingIntent{}
	var order []string
	for rows.Next() {
		var id int64
		var ticketID, payloadRaw string
		if err := rows.Scan(&id, &ticketID, &payloadRaw); err != nil {
			return nil, nil, fmt.Errorf("scan launch intent: %w", err)
		}
		var payload launchPayload
		if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
			return nil, nil, fmt.Errorf("decode launch intent payload for %s: %w", ticketID, err)
		}
		if _, seen := groups[payload.Group]; !seen {
			order = append(order, payload.Group)
		}
		groups[payload.Group] = append(groups[payload.Group],
			pendingIntent{id: id, ticketID: ticketID, promptHash: payload.PromptHash})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate launch intents: %w", err)
	}
	return groups, order, nil
}

func insertLaunch(ctx context.Context, tx dbTx, at string, members []pendingIntent) error {
	res, err := tx.ExecContext(ctx, `INSERT INTO launches (created_at, state) VALUES (?, 'active')`, at)
	if err != nil {
		return fmt.Errorf("insert launch: %w", err)
	}
	launchID, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("launch id: %w", err)
	}

	for _, m := range members {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO launch_members (launch_id, ticket_id, prompt_hash) VALUES (?, ?, ?)`,
			launchID, m.ticketID, m.promptHash); err != nil {
			return fmt.Errorf("insert launch member %s: %w", m.ticketID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE intents SET consumed_at = ? WHERE id = ?`, at, m.id); err != nil {
			return fmt.Errorf("consume intent %d: %w", m.id, err)
		}
	}

	detail := fmt.Sprintf("launch %d authorised with %d member(s)", launchID, len(members))
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events (at, ticket_id, kind, detail) VALUES (?, NULL, 'launch', ?)`, at, detail); err != nil {
		return fmt.Errorf("append launch event: %w", err)
	}
	return nil
}

type LaunchMembership struct {
	LaunchID   int64
	Members    int
	Cancelled  bool
	PromptHash string
}

// LaunchMemberships returns every ticket in an active launch, keyed by ticket URL, plus that
// launch's member count — and Cancelled for a ticket whose launch was cancelled and not relaunched.
func (s *Store) LaunchMemberships(ctx context.Context) (map[string]LaunchMembership, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT lm.ticket_id, lm.launch_id, l.state, lm.prompt_hash,
		       COUNT(*) OVER (PARTITION BY lm.launch_id) AS members
		FROM launch_members lm
		JOIN launches l ON l.id = lm.launch_id
		WHERE l.state IN ('active', 'cancelled')`)
	if err != nil {
		return nil, fmt.Errorf("select launch memberships: %w", err)
	}
	defer func() { _ = rows.Close() }()

	memberships := map[string]LaunchMembership{}
	cancelled := map[string]bool{}
	for rows.Next() {
		var ticketID, state string
		var m LaunchMembership
		if err := rows.Scan(&ticketID, &m.LaunchID, &state, &m.PromptHash, &m.Members); err != nil {
			return nil, fmt.Errorf("scan launch membership: %w", err)
		}
		if state == "cancelled" {
			cancelled[ticketID] = true
			continue
		}
		memberships[ticketID] = m
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate launch memberships: %w", err)
	}
	for ticketID := range cancelled {
		if _, active := memberships[ticketID]; !active {
			memberships[ticketID] = LaunchMembership{Cancelled: true}
		}
	}
	return memberships, nil
}

// CancelLaunchesFor cancels every active launch the ticket belongs to, returning how many
// memberships those launches withdraw — the named ticket's own included.
func (s *Store) CancelLaunchesFor(ctx context.Context, ticketID string) (members int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()

	const activeLaunches = `
		SELECT lm.launch_id FROM launch_members lm
		JOIN launches l ON l.id = lm.launch_id
		WHERE l.state = 'active' AND lm.ticket_id = ?`

	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM launch_members WHERE launch_id IN (`+activeLaunches+`)`, ticketID).Scan(&members)
	if err != nil {
		return 0, fmt.Errorf("count members of active launches for %s: %w", ticketID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE launches SET state = 'cancelled' WHERE id IN (`+activeLaunches+`)`, ticketID); err != nil {
		return 0, fmt.Errorf("cancel launches for %s: %w", ticketID, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return members, nil
}
