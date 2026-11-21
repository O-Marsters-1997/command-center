package store

import (
	"context"
	"fmt"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// FeatureClosureError rejects an import or edit blocked by an unmerged ticket in another feature.
type FeatureClosureError struct {
	Feature, URL, Blocker, BlockerFeature string
}

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
	URL       string   `json:"url"`
	BlockedBy []string `json:"blocked_by"`
}

func closeUnderBlockedBy(
	feature string, tickets []ticketBlockedBy, featureOf map[string]string, merged map[string]bool,
) *FeatureClosureError {
	for _, t := range tickets {
		for _, blocker := range t.BlockedBy {
			if featureOf[blocker] == feature || merged[blocker] {
				continue
			}
			return &FeatureClosureError{Feature: feature, URL: t.URL, Blocker: blocker, BlockerFeature: featureOf[blocker]}
		}
	}
	return nil
}

func checkClosure(
	ctx context.Context, qtx *ccdb.Queries, feature string, tickets []ticketBlockedBy, obs plan.Observation,
) error {
	featureOf, merged, err := closureLookups(ctx, qtx, tickets, obs)
	if err != nil {
		return err
	}
	if violation := closeUnderBlockedBy(feature, tickets, featureOf, merged); violation != nil {
		return violation
	}
	return nil
}

func closureLookups(
	ctx context.Context, qtx *ccdb.Queries, tickets []ticketBlockedBy, obs plan.Observation,
) (map[string]string, map[string]bool, error) {
	seen := map[string]bool{}
	blockers := []string{}
	for _, t := range tickets {
		for _, blocker := range t.BlockedBy {
			if !seen[blocker] {
				seen[blocker] = true
				blockers = append(blockers, blocker)
			}
		}
	}
	urls, err := encodeJSON("blocker urls", blockers)
	if err != nil {
		return nil, nil, err
	}
	featureRows, err := qtx.ListTicketFeatures(ctx, urls)
	if err != nil {
		return nil, nil, fmt.Errorf("read blocker features: %w", err)
	}
	branchRows, err := qtx.ListTicketBranches(ctx, urls)
	if err != nil {
		return nil, nil, fmt.Errorf("read blocker branches: %w", err)
	}
	features := make(map[string]string, len(featureRows))
	for _, row := range featureRows {
		features[row.URL] = row.Feature
	}
	merged := make(map[string]bool, len(branchRows))
	for _, row := range branchRows {
		merged[row.URL] = obs.PRs[plan.BranchRef{Repo: row.Repo, Branch: row.Branch}].State == plan.Merged
	}
	return features, merged, nil
}

func featureTicketsWithOverride(
	ctx context.Context, qtx *ccdb.Queries, feature string, edit ticketBlockedBy,
) ([]ticketBlockedBy, error) {
	rows, err := qtx.ListTicketsInFeature(ctx, feature)
	if err != nil {
		return nil, fmt.Errorf("list existing tickets for %s: %w", feature, err)
	}
	tickets := make([]ticketBlockedBy, 0, len(rows))
	for _, row := range rows {
		blockedBy := slices.Clone(edit.BlockedBy)
		if row.URL != edit.URL {
			blockedBy, err = decodeBlockedBy(row.URL, row.BlockedBy)
			if err != nil {
				return nil, err
			}
		}
		tickets = append(tickets, ticketBlockedBy{URL: row.URL, BlockedBy: blockedBy})
	}
	return tickets, nil
}
