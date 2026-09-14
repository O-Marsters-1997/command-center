package cc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc/ccdb"
)

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
	err = s.q.QueueLaunchIntent(ctx, ccdb.QueueLaunchIntentParams{
		At:       at.UTC().Format(time.RFC3339Nano),
		TicketID: ticketID,
		Payload:  notNull(string(payload)),
	})
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

	qtx := s.q.WithTx(tx)
	groups, order, err := pendingLaunchIntents(ctx, qtx)
	if err != nil {
		return err
	}

	nowStr := now.UTC().Format(time.RFC3339Nano)
	for _, group := range order {
		if err := insertLaunch(ctx, qtx, nowStr, groups[group]); err != nil {
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
func pendingLaunchIntents(ctx context.Context, q *ccdb.Queries) (map[string][]pendingIntent, []string, error) {
	rows, err := q.PendingLaunchIntents(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("select launch intents: %w", err)
	}

	groups := map[string][]pendingIntent{}
	var order []string
	for _, row := range rows {
		var payload launchPayload
		if err := json.Unmarshal([]byte(row.Payload.String), &payload); err != nil {
			return nil, nil, fmt.Errorf("decode launch intent payload for %s: %w", row.TicketID, err)
		}
		if _, seen := groups[payload.Group]; !seen {
			order = append(order, payload.Group)
		}
		groups[payload.Group] = append(groups[payload.Group],
			pendingIntent{id: row.ID, ticketID: row.TicketID, promptHash: payload.PromptHash})
	}
	return groups, order, nil
}

func insertLaunch(ctx context.Context, q *ccdb.Queries, at string, members []pendingIntent) error {
	launchID, err := q.InsertLaunch(ctx, at)
	if err != nil {
		return fmt.Errorf("insert launch: %w", err)
	}

	for _, m := range members {
		if err := q.InsertLaunchMember(ctx, ccdb.InsertLaunchMemberParams{
			LaunchID: launchID, TicketID: m.ticketID, PromptHash: m.promptHash,
		}); err != nil {
			return fmt.Errorf("insert launch member %s: %w", m.ticketID, err)
		}
		if err := q.ConsumeLaunchIntent(ctx, ccdb.ConsumeLaunchIntentParams{
			ConsumedAt: notNull(at), ID: m.id,
		}); err != nil {
			return fmt.Errorf("consume intent %d: %w", m.id, err)
		}
	}

	detail := fmt.Sprintf("launch %d authorised with %d member(s)", launchID, len(members))
	if err := q.InsertLaunchEvent(ctx, ccdb.InsertLaunchEventParams{At: at, Detail: notNull(detail)}); err != nil {
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
	rows, err := s.q.LaunchMemberships(ctx)
	if err != nil {
		return nil, fmt.Errorf("select launch memberships: %w", err)
	}

	memberships := map[string]LaunchMembership{}
	cancelled := map[string]bool{}
	for _, row := range rows {
		if row.State == "cancelled" {
			cancelled[row.TicketID] = true
			continue
		}
		memberships[row.TicketID] = LaunchMembership{
			LaunchID:   row.LaunchID,
			Members:    int(row.Members),
			PromptHash: row.PromptHash,
		}
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

	qtx := s.q.WithTx(tx)
	count, err := qtx.ActiveLaunchMemberCount(ctx, ticketID)
	if err != nil {
		return 0, fmt.Errorf("count members of active launches for %s: %w", ticketID, err)
	}
	if err := qtx.CancelActiveLaunches(ctx, ticketID); err != nil {
		return 0, fmt.Errorf("cancel launches for %s: %w", ticketID, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return int(count), nil
}
