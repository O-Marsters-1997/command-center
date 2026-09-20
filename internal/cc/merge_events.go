package cc

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/gh"
)

const eventPRMerged = "pr_merged"

func (l *Loop) recordMergedEvents(ctx context.Context, obs Observation) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}

	for _, t := range tickets {
		pr := obs.PRs[branchKey(t.Repo, t.Branch)]
		if pr.State != gh.Merged {
			continue
		}
		exists, err := l.store.HasEvent(ctx, t.URL, eventPRMerged)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if err := l.store.AppendEvent(ctx, Event{
			At: pr.MergedAt, TicketURL: t.URL, Kind: eventPRMerged,
			Detail: fmt.Sprintf("PR #%d merged", pr.Number),
		}); err != nil {
			return err
		}
	}
	return nil
}
