package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

type launchPayload struct {
	PromptHash string `json:"prompt_hash"`
	Group      string `json:"group"`
}

// QueueLaunchIntent records one ticket's authorisation to launch.
func (s *Store) QueueLaunchIntent(ctx context.Context, l LaunchRequest) error {
	payload, err := json.Marshal(launchPayload{PromptHash: l.PromptHash, Group: l.Group})
	if err != nil {
		return fmt.Errorf("encode launch intent payload for %s: %w", l.Ticket, err)
	}
	err = s.q.QueueLaunchIntent(ctx, ccdb.QueueLaunchIntentParams{
		At:       l.At.UTC(),
		TicketID: l.Ticket,
		Payload:  notNull(string(payload)),
	})
	if err != nil {
		return fmt.Errorf("queue launch intent for %s: %w", l.Ticket, err)
	}
	return nil
}

// ApplyLaunchIntents turns every unconsumed launch intent into a launches row per group
// plus a launch_members row per intent, marking each consumed.
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
		return fmt.Errorf("read pending launch intents: %w", err)
	}

	nowUTC := now.UTC()
	for _, group := range order {
		if err := insertLaunch(ctx, qtx, nowUTC, groups[group]); err != nil {
			return fmt.Errorf("apply launch group %s: %w", group, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit launch intents: %w", err)
	}
	return nil
}

type pendingIntent struct {
	id         int64
	ticketID   string
	promptHash string
}

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

func insertLaunch(ctx context.Context, q *ccdb.Queries, at time.Time, members []pendingIntent) error {
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
			ConsumedAt: notNullTime(at), ID: m.id,
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

// LaunchMemberships returns every ticket in an active launch, keyed by ticket URL, plus a
// Cancelled entry for a ticket whose launch was cancelled and not relaunched.
func (s *Store) LaunchMemberships(ctx context.Context) (map[plan.TicketURL]plan.LaunchMembership, error) {
	rows, err := s.q.LaunchMemberships(ctx)
	if err != nil {
		return nil, fmt.Errorf("select launch memberships: %w", err)
	}

	memberships := map[plan.TicketURL]plan.LaunchMembership{}
	cancelled := map[plan.TicketURL]bool{}
	for _, row := range rows {
		if row.State == "cancelled" {
			cancelled[plan.TicketURL(row.TicketID)] = true
			continue
		}
		memberships[plan.TicketURL(row.TicketID)] = plan.LaunchMembership{
			LaunchID:   row.LaunchID,
			Members:    int(row.Members),
			PromptHash: row.PromptHash,
		}
	}
	for ticketID := range cancelled {
		if _, active := memberships[ticketID]; !active {
			memberships[ticketID] = plan.LaunchMembership{Cancelled: true}
		}
	}
	return memberships, nil
}

// CancelLaunchesFor cancels every active launch the ticket belongs to, returning how many
// memberships it withdraws.
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

// FeatureFirstLaunch returns when the earliest launch holding a ticket of feature was created;
// ok is false when none has launched.
func (s *Store) FeatureFirstLaunch(ctx context.Context, feature string) (at time.Time, ok bool, err error) {
	at, err = s.q.FeatureFirstLaunch(ctx, feature)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("select first launch of feature %q: %w", feature, err)
	}
	return at, true, nil
}
