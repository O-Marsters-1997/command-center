package store

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// RefreshFacts returns every ticket's outstanding refresh-domain problem, keyed by ticket URL.
// A refusal or verification failure gates the automatic retry; the refresh verb ignores it.
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

// RefreshOutcome is one ticket's latest refresh-domain event.
type RefreshOutcome struct {
	Kind   EventKind
	Detail string
}

// LatestRefreshOutcomes returns each ticket's latest refresh-domain event since its last
// recorded push, keyed by ticket URL.
func (s *Store) LatestRefreshOutcomes(ctx context.Context) (map[plan.TicketURL]RefreshOutcome, error) {
	rows, err := s.q.LatestRefreshOutcomes(ctx, ccdb.LatestRefreshOutcomesParams{
		Kind:   string(EventRefreshRefused),
		Kind_2: string(EventRefreshConflicted),
		Kind_3: string(EventVerificationFailed),
		Kind_4: string(EventRefreshed),
		Kind_5: string(EventRestacked),
	})
	if err != nil {
		return nil, fmt.Errorf("select refresh outcomes: %w", err)
	}

	outcomes := map[plan.TicketURL]RefreshOutcome{}
	for _, row := range rows {
		if !row.TicketID.Valid {
			continue
		}
		kind, err := ParseEventKind(row.Kind)
		if err != nil {
			return nil, fmt.Errorf("read events.kind for %s: %w", row.TicketID.String, err)
		}
		outcomes[plan.TicketURL(row.TicketID.String)] = RefreshOutcome{Kind: kind, Detail: row.Detail.String}
	}
	return outcomes, nil
}
