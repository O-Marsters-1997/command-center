package cc

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc/ccdb"
)

// RecordPush writes one successful push -- the row Phase 4's crash-safety hinges on:
// pushed_tip is compared against on every later tick's plan.PushPlan, so a duplicate push or a
// duplicate PR create both stop the moment this lands (inv. 20).
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
	return s.resetCheckingTicks(ctx, ticketID)
}

// RestackedSinceLastPush names every ticket whose branch the app itself rebased since it last
// recorded a push of it, which is the only licence the push step has to lease-force (issue #89).
// The comparison is >= rather than >: retargetOne stamps its push row and the restack that
// follows it with one tick's single clock reading, so a strict > would never see its own work.
func (s *Store) RestackedSinceLastPush(ctx context.Context) (map[string]bool, error) {
	rows, err := s.q.RestackedSinceLastPush(ctx, eventRestacked)
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

// LastPushedTips returns each ticket's most recently recorded pushed_tip -- what plan.PushPlan
// compares a branch's current local tip against.
func (s *Store) LastPushedTips(ctx context.Context) (map[string]string, error) {
	rows, err := s.q.LastPushedTips(ctx)
	if err != nil {
		return nil, fmt.Errorf("select last pushed tips: %w", err)
	}

	tips := map[string]string{}
	for _, row := range rows {
		tips[row.TicketID] = row.PushedTip
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
	rows, err := s.q.LatestPushes(ctx)
	if err != nil {
		return nil, fmt.Errorf("select latest pushes: %w", err)
	}

	pushes := map[string]PushRow{}
	for _, r := range rows {
		pushes[r.TicketID] = PushRow{
			PushedTip:     r.PushedTip,
			BaseBranch:    r.BaseBranch,
			BaseSHAAtPush: r.BaseSHAAtPush,
			PushedAt:      r.PushedAt,
		}
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
	rows, err := s.q.PushFacts(ctx, ccdb.PushFactsParams{Kind: eventPushRefused, Kind_2: eventPushFailed})
	if err != nil {
		return nil, fmt.Errorf("select push facts: %w", err)
	}

	facts := map[string]PushFact{}
	for _, row := range rows {
		if !row.TicketID.Valid {
			continue
		}
		switch row.Kind {
		case eventPushRefused:
			facts[row.TicketID.String] = PushFact{Refused: true, RefusedPath: row.Detail.String}
		case eventPushFailed:
			facts[row.TicketID.String] = PushFact{Failed: true}
		}
	}
	return facts, nil
}
