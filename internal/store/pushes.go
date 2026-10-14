package store

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// RecordPush writes one successful push row; its pushed_tip is what later ticks
// compare against, so a duplicate push or PR create stops once it lands.
func (s *Store) RecordPush(ctx context.Context, ticketID, pushedTip, baseBranch, baseSHA string, at time.Time) error {
	err := s.q.RecordPush(ctx, ccdb.RecordPushParams{
		TicketID:      ticketID,
		PushedTip:     pushedTip,
		BaseBranch:    baseBranch,
		BaseSHAAtPush: baseSHA,
		PushedAt:      at.UTC(),
	})
	if err != nil {
		return fmt.Errorf("record push for %s: %w", ticketID, err)
	}
	return s.ResetCheckingTicks(ctx, ticketID)
}

// RestackedSinceLastPush names every ticket whose branch the app rebased since it last
// recorded a push of it, the only licence the push step has to lease-force.
// The comparison is >= because a retarget stamps its push and restack with one clock reading.
func (s *Store) RestackedSinceLastPush(ctx context.Context) (map[string]bool, error) {
	rows, err := s.q.RestackedSinceLastPush(ctx, EventRestacked)
	if err != nil {
		return nil, fmt.Errorf("select restacked tickets: %w", err)
	}

	restacked := map[string]bool{}
	for _, ticketID := range rows {
		if !ticketID.Valid {
			continue
		}
		restacked[ticketID.String] = true
	}
	return restacked, nil
}

// LastPushedTips returns each ticket's most recently recorded pushed_tip.
func (s *Store) LastPushedTips(ctx context.Context) (map[string]string, error) {
	pushes, err := s.LatestPushes(ctx)
	if err != nil {
		return nil, err
	}

	tips := make(map[string]string, len(pushes))
	for ticketID, push := range pushes {
		tips[ticketID] = push.PushedTip
	}
	return tips, nil
}

// LatestPushes returns each ticket's latest recorded push, keyed by ticket URL.
func (s *Store) LatestPushes(ctx context.Context) (map[string]plan.PushRow, error) {
	rows, err := s.q.ListLatestPushes(ctx)
	if err != nil {
		return nil, fmt.Errorf("select latest pushes: %w", err)
	}

	pushes := map[string]plan.PushRow{}
	for _, r := range rows {
		pushes[r.TicketID] = plan.PushRow{
			PushedTip:     r.PushedTip,
			BaseBranch:    r.BaseBranch,
			BaseSHAAtPush: r.BaseSHAAtPush,
			PushedAt:      r.PushedAt,
		}
	}
	return pushes, nil
}

// Event kinds a push writes and PushFacts reads back.
const (
	EventPushRefused = "push_refused"
	EventPushFailed  = "push_failed"
	EventPushed      = "pushed"
)

// PushFacts returns every ticket's outstanding push-policy problem, keyed by ticket URL.
// A failure blocks the automatic retry; a refusal never does.
func (s *Store) PushFacts(ctx context.Context) (map[string]plan.PushFact, error) {
	kinds, err := encodeJSON("push fact kinds", []string{EventPushRefused, EventPushFailed})
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListPushFacts(ctx, kinds)
	if err != nil {
		return nil, fmt.Errorf("select push facts: %w", err)
	}

	facts := map[string]plan.PushFact{}
	for _, row := range rows {
		if !row.TicketID.Valid {
			continue
		}
		switch row.Kind {
		case EventPushRefused:
			facts[row.TicketID.String] = plan.PushFact{Refused: true, RefusedPath: row.Detail}
		case EventPushFailed:
			facts[row.TicketID.String] = plan.PushFact{Failed: true}
		}
	}
	return facts, nil
}
