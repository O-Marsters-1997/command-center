package store

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// RecordPush writes one successful push row; its pushed_tip is what later ticks
// compare against, so a duplicate push or PR create stops once it lands.
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

// RestackedSinceLastPush names every ticket whose branch the app rebased since it last
// recorded a push of it, the only licence the push step has to lease-force.
// The comparison is >= because a retarget stamps its push and restack with one clock reading.
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

// LastPushedTips returns each ticket's most recently recorded pushed_tip.
func (s *Store) LastPushedTips(ctx context.Context) (map[plan.TicketURL]plan.SHA, error) {
	rows, err := s.q.LastPushedTips(ctx)
	if err != nil {
		return nil, fmt.Errorf("select last pushed tips: %w", err)
	}

	tips := map[plan.TicketURL]plan.SHA{}
	for _, row := range rows {
		tips[plan.TicketURL(row.TicketID)] = plan.SHA(row.PushedTip)
	}
	return tips, nil
}

// LatestPushes returns each ticket's latest recorded push, keyed by ticket URL.
func (s *Store) LatestPushes(ctx context.Context) (map[plan.TicketURL]plan.PushRow, error) {
	rows, err := s.q.LatestPushes(ctx)
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

// PushFacts returns every ticket's outstanding push-policy problem, keyed by ticket URL.
// A failure blocks the automatic retry; a refusal never does.
func (s *Store) PushFacts(ctx context.Context) (map[plan.TicketURL]plan.PushFact, error) {
	rows, err := s.q.PushFacts(ctx, ccdb.PushFactsParams{
		Kind: string(EventPushRefused), Kind_2: string(EventPushFailed),
	})
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
			facts[plan.TicketURL(row.TicketID.String)] = plan.PushFact{Refused: true, RefusedPath: row.Detail.String}
		}
		if kind == EventPushFailed {
			facts[plan.TicketURL(row.TicketID.String)] = plan.PushFact{Failed: true}
		}
	}
	return facts, nil
}
