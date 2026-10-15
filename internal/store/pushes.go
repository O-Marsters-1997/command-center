package store

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// RecordPush persists the tip against which later ticks suppress duplicate pushes and PR creation.
func (s *Store) RecordPush(ctx context.Context, p Push) error {
	err := s.q.RecordPush(ctx, ccdb.RecordPushParams{
		TicketID:      string(p.Ticket),
		PushedTip:     string(p.Tip),
		BaseBranch:    p.BaseBranch,
		BaseSHAAtPush: string(p.BaseSHA),
		PushedAt:      p.At.UTC(),
	})
	if err != nil {
		return fmt.Errorf("record push for %s: %w", p.Ticket, err)
	}
	if err := s.ResetCheckingTicks(ctx, p.Ticket); err != nil {
		return fmt.Errorf("reset checking ticks for %s: %w", p.Ticket, err)
	}
	return nil
}

// RestackedSinceLastPush authorizes lease-force pushes when a restack is at or after the last push.
func (s *Store) RestackedSinceLastPush(ctx context.Context) (map[plan.TicketURL]bool, error) {
	rows, err := s.q.RestackedSinceLastPush(ctx, string(EventRestacked))
	if err != nil {
		return nil, fmt.Errorf("select restacked tickets: %w", err)
	}

	restacked := map[plan.TicketURL]bool{}
	for _, ticketID := range rows {
		if !ticketID.Valid {
			continue
		}
		restacked[plan.TicketURL(ticketID.String)] = true
	}
	return restacked, nil
}

// LastPushedTips returns the latest recorded pushed tip per ticket.
func (s *Store) LastPushedTips(ctx context.Context) (map[plan.TicketURL]plan.SHA, error) {
	pushes, err := s.LatestPushes(ctx)
	if err != nil {
		return nil, err
	}

	tips := make(map[plan.TicketURL]plan.SHA, len(pushes))
	for ticketID, push := range pushes {
		tips[ticketID] = push.PushedTip
	}
	return tips, nil
}

// LatestPushes returns the latest recorded push per ticket URL.
func (s *Store) LatestPushes(ctx context.Context) (map[plan.TicketURL]plan.PushRow, error) {
	rows, err := s.q.ListLatestPushes(ctx)
	if err != nil {
		return nil, fmt.Errorf("select latest pushes: %w", err)
	}

	pushes := map[plan.TicketURL]plan.PushRow{}
	for _, r := range rows {
		pushes[plan.TicketURL(r.TicketID)] = plan.PushRow{
			PushedTip:     plan.SHA(r.PushedTip),
			BaseBranch:    r.BaseBranch,
			BaseSHAAtPush: plan.SHA(r.BaseSHAAtPush),
			PushedAt:      r.PushedAt,
		}
	}
	return pushes, nil
}

// PushFacts blocks automatic retries after failures but not refusals.
func (s *Store) PushFacts(ctx context.Context) (map[plan.TicketURL]plan.PushFact, error) {
	kinds, err := encodeJSON("push fact kinds", []EventKind{EventPushRefused, EventPushFailed})
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListPushFacts(ctx, kinds)
	if err != nil {
		return nil, fmt.Errorf("select push facts: %w", err)
	}

	facts := map[plan.TicketURL]plan.PushFact{}
	for _, row := range rows {
		if !row.TicketID.Valid {
			continue
		}
		kind, err := ParseEventKind(row.Kind)
		if err != nil {
			return nil, fmt.Errorf("read events.kind for %s: %w", row.TicketID.String, err)
		}
		if kind == EventPushRefused {
			facts[plan.TicketURL(row.TicketID.String)] = plan.PushFact{Refused: true, RefusedPath: row.Detail}
		}
		if kind == EventPushFailed {
			facts[plan.TicketURL(row.TicketID.String)] = plan.PushFact{Failed: true}
		}
	}
	return facts, nil
}
