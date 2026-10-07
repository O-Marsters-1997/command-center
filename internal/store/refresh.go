package store

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// Event kinds the refresh domain writes and RefreshFacts reads back.
const (
	EventRefreshRefused     = "refresh_refused"
	EventRefreshConflicted  = "refresh_conflicted"
	EventRefreshed          = "refreshed"
	EventRestacked          = "restacked"
	EventVerificationFailed = "verification_failed"
)

// RefreshFacts returns every ticket's outstanding refresh-domain problem, keyed by ticket URL. A
// refusal or a verification failure gates the automatic pass's retry; the refresh verb ignores it
// (docs/designs/command-centre-design.md § 4a).
func (s *Store) RefreshFacts(ctx context.Context) (map[string]plan.RefreshFact, error) {
	outcomes, err := s.LatestRefreshOutcomes(ctx)
	if err != nil {
		return nil, err
	}
	facts := make(map[string]plan.RefreshFact, len(outcomes))
	for ticketID, o := range outcomes {
		switch o.Kind {
		case EventRefreshRefused:
			facts[ticketID] = plan.RefreshFact{Refused: true, Reason: o.Detail}
		case EventVerificationFailed:
			facts[ticketID] = plan.RefreshFact{VerificationFailed: true, VerificationFailedDetail: o.Detail}
		}
	}
	return facts, nil
}

// RefreshOutcome is one ticket's latest refresh-domain event: its kind and detail.
type RefreshOutcome struct{ Kind, Detail string }

// LatestRefreshOutcomes returns each ticket's latest refresh-domain event since its last recorded
// push, keyed by ticket URL: a failure the automatic pass never retries, or a later success that
// supersedes an earlier failure without needing its own push
// (docs/designs/command-centre-design.md § 4a).
func (s *Store) LatestRefreshOutcomes(ctx context.Context) (map[string]RefreshOutcome, error) {
	rows, err := s.q.LatestRefreshOutcomes(ctx, ccdb.LatestRefreshOutcomesParams{
		Kind:   EventRefreshRefused,
		Kind_2: EventRefreshConflicted,
		Kind_3: EventVerificationFailed,
		Kind_4: EventRefreshed,
		Kind_5: EventRestacked,
	})
	if err != nil {
		return nil, fmt.Errorf("select refresh outcomes: %w", err)
	}

	outcomes := map[string]RefreshOutcome{}
	for _, row := range rows {
		if !row.TicketID.Valid {
			continue
		}
		outcomes[row.TicketID.String] = RefreshOutcome{Kind: row.Kind, Detail: row.Detail.String}
	}
	return outcomes, nil
}

// applyRefreshIntents runs every requested refresh, which bypasses the RefreshFacts gate the way
