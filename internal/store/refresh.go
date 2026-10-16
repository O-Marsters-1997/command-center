package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/option"
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
		fact := plan.RefreshFact{Tried: true}
		if o.Kind == EventRefreshRefused {
			fact.Refused, fact.Reason = true, o.Detail
		}
		if o.Kind == EventVerificationFailed {
			fact.VerificationFailed, fact.VerificationFailedDetail = true, o.Detail
		}
		if at, ok := parseConflictDetail(o.Detail); ok && o.Kind == EventRefreshConflicted {
			fact.Conflict = option.Some(at)
		}
		facts[ticketID] = fact
	}
	return facts, nil
}

func parseConflictDetail(detail string) (plan.ConflictAt, bool) {
	fields := strings.SplitN(detail, " ", 3)
	if len(fields) < 3 {
		return plan.ConflictAt{}, false
	}
	return plan.ConflictAt{BranchTip: plan.SHA(fields[0]), BaseTip: plan.SHA(fields[1])}, true
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
		if !row.TicketID.Valid {
			continue
		}
		kind, err := ParseEventKind(row.Kind)
		if err != nil {
			return nil, fmt.Errorf("read events.kind for %s: %w", row.TicketID.String, err)
		}
		outcomes[plan.TicketURL(row.TicketID.String)] = RefreshOutcome{Kind: kind, Detail: row.Detail}
	}
	return outcomes, nil
}
