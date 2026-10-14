package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// FeatureClosureError reports a ticket blocked by an unmerged ticket outside its own
// feature; the import or edit is refused.
type FeatureClosureError struct {
	Feature, URL, Blocker, BlockerFeature string
}

func (e *FeatureClosureError) refusedTicket() string { return "" }

func (e *FeatureClosureError) Error() string {
	if e.BlockerFeature == "" {
		return fmt.Sprintf(
			"ticket %s is blocked by %s, which is not in feature %q or any other, refusing to import it",
			e.URL, e.Blocker, e.Feature,
		)
	}
	return fmt.Sprintf(
		"ticket %s is blocked by %s, which belongs to feature %q, not %q",
		e.URL, e.Blocker, e.BlockerFeature, e.Feature,
	)
}

type ticketBlockedBy struct {
	URL       string
	BlockedBy []string
}

func closeUnderBlockedBy(
	feature string,
	tickets []ticketBlockedBy,
	ticketFeature func(url string) (string, error),
	merged func(url string) (bool, error),
) (*FeatureClosureError, error) {
	for _, t := range tickets {
		for _, blocker := range t.BlockedBy {
			blockerFeature, err := ticketFeature(blocker)
			if err != nil {
				return nil, fmt.Errorf("read feature of blocker %s: %w", blocker, err)
			}
			if blockerFeature == feature {
				continue
			}
			isMerged, err := merged(blocker)
			if err != nil {
				return nil, fmt.Errorf("read merged state of blocker %s: %w", blocker, err)
			}
			if isMerged {
				continue
			}
			return &FeatureClosureError{Feature: feature, URL: t.URL, Blocker: blocker, BlockerFeature: blockerFeature}, nil
		}
	}
	return nil, nil
}

func ticketFeatureLookup(ctx context.Context, qtx *ccdb.Queries) func(url string) (string, error) {
	return func(url string) (string, error) {
		feature, err := qtx.TicketFeatureAny(ctx, url)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("read feature for %s: %w", url, err)
		}
		return feature, nil
	}
}

func blockerMergedLookup(ctx context.Context, qtx *ccdb.Queries, obs plan.Observation) func(url string) (bool, error) {
	return func(url string) (bool, error) {
		branch, err := qtx.TicketBranch(ctx, url)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("read branch for %s: %w", url, err)
		}
		return obs.PRs[plan.BranchRef{Repo: branch.Repo, Branch: branch.Branch}].State == plan.Merged, nil
	}
}

func featureTicketsWithOverride(
	ctx context.Context, qtx *ccdb.Queries, feature, overrideURL string, override []string,
) ([]ticketBlockedBy, error) {
	rows, err := qtx.TicketsInFeature(ctx, feature)
	if err != nil {
		return nil, fmt.Errorf("list existing tickets for %s: %w", feature, err)
	}
	tickets := make([]ticketBlockedBy, 0, len(rows))
	for _, row := range rows {
		blockedBy := override
		if row.URL != overrideURL {
			// json.Unmarshal reuses a destination slice's capacity, so decoding into override
			// would overwrite the edit under validation.
			decoded, err := decodeBlockedBy(row.BlockedBy)
			if err != nil {
				return nil, fmt.Errorf("decode blocked_by for %s: %w", row.URL, err)
			}
			blockedBy = decoded
		}
		tickets = append(tickets, ticketBlockedBy{URL: row.URL, BlockedBy: blockedBy})
	}
	return tickets, nil
}
