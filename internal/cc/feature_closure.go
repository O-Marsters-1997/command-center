package cc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/cc/ccdb"
	"github.com/O-Marsters-1997/command-center/internal/gh"
)

// FeatureClosureError reports a ticket blocked by a ticket outside its own feature, whose pull
// request has not merged: a feature is closed under blocked_by, or the import/edit refuses.
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
	ticketFeature func(url string) string,
	merged func(url string) bool,
) *FeatureClosureError {
	for _, t := range tickets {
		for _, blocker := range t.BlockedBy {
			blockerFeature := ticketFeature(blocker)
			if blockerFeature == feature || merged(blocker) {
				continue
			}
			return &FeatureClosureError{Feature: feature, URL: t.URL, Blocker: blocker, BlockerFeature: blockerFeature}
		}
	}
	return nil
}

func ticketFeatureLookup(ctx context.Context, qtx *ccdb.Queries) func(url string) string {
	return func(url string) string {
		feature, _ := qtx.TicketFeatureAny(ctx, url)
		return feature
	}
}

func blockerMergedLookup(ctx context.Context, qtx *ccdb.Queries, obs Observation) func(url string) bool {
	return func(url string) bool {
		branch, err := qtx.TicketBranch(ctx, url)
		if err != nil {
			return false
		}
		return obs.PRs[branchKey(branch.Repo, branch.Branch)].State == gh.Merged
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
			if err := json.Unmarshal(row.BlockedBy, &blockedBy); err != nil {
				return nil, fmt.Errorf("decode blocked_by for %s: %w", row.URL, err)
			}
		}
		tickets = append(tickets, ticketBlockedBy{URL: row.URL, BlockedBy: blockedBy})
	}
	return tickets, nil
}
