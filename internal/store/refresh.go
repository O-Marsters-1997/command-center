package store

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// RefreshFacts gates automatic retries after refusal or verification failure, but not manual refreshes.
func (s *Store) RefreshFacts(ctx context.Context) (map[plan.TicketURL]plan.RefreshFact, error) {
	outcomes, err := s.LatestRefreshOutcomes(ctx)
	if err != nil {
		return nil, fmt.Errorf("read latest refresh outcomes: %w", err)
	}
	facts := make(map[plan.TicketURL]plan.RefreshFact, len(outcomes))
	for ticketID, o := range outcomes {
		if o.Kind == EventRefreshRefused {
			facts[ticketID] = plan.RefreshFact{Refused: true, Reason: o.Detail}
		}
		if o.Kind == EventVerificationFailed {
			facts[ticketID] = plan.RefreshFact{VerificationFailed: true, VerificationFailedDetail: o.Detail}
		}
	}
	return facts, nil
}

// RefreshOutcome holds a ticket's latest refresh-domain event.
type RefreshOutcome struct {
	Kind   EventKind
	Detail string
}

// LatestRefreshOutcomes excludes events before the ticket's last recorded push.
func (s *Store) LatestRefreshOutcomes(ctx context.Context) (map[plan.TicketURL]RefreshOutcome, error) {
	kinds, err := encodeJSON("refresh outcome kinds", []EventKind{
		EventRefreshRefused, EventRefreshConflicted, EventVerificationFailed, EventRefreshed, EventRestacked,
	})
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListLatestRefreshOutcomes(ctx, kinds)
	if err != nil {
		return nil, fmt.Errorf("select refresh outcomes: %w", err)
	}

	outcomes := map[plan.TicketURL]RefreshOutcome{}
	for _, row := range rows {
		kind, err := ParseEventKind(row.Kind)
		if err != nil {
			return nil, fmt.Errorf("read events.kind for %s: %w", row.TicketID, err)
		}
		outcomes[plan.TicketURL(row.TicketID)] = RefreshOutcome{Kind: kind, Detail: row.Detail}
	}
	return outcomes, nil
}
