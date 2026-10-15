package store

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// FeatureClosureError rejects an import or edit blocked by an unmerged ticket in another feature.
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

func closureLookups(
	ctx context.Context, qtx *ccdb.Queries, tickets []ticketBlockedBy, obs plan.Observation,
) (func(url string) (string, error), func(url string) (bool, error), error) {
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
	featureOf := func(url string) (string, error) { return features[url], nil }
	isMerged := func(url string) (bool, error) { return merged[url], nil }
	return featureOf, isMerged, nil
}

type blockedByOverride struct {
	URL       string
	BlockedBy []string
}

func featureTicketsWithOverride(
	ctx context.Context, qtx *ccdb.Queries, feature string, edit blockedByOverride,
) ([]ticketBlockedBy, error) {
	rows, err := qtx.ListTicketsInFeature(ctx, feature)
	if err != nil {
		return nil, fmt.Errorf("list existing tickets for %s: %w", feature, err)
	}
	tickets := make([]ticketBlockedBy, 0, len(rows))
	for _, row := range rows {
		blockedBy := edit.BlockedBy
		if row.URL != edit.URL {
			// json.Unmarshal can reuse a slice's backing array and overwrite the edit under validation.
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
