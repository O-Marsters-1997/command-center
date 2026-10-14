package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) recordMergedEvents(ctx context.Context, obs plan.Observation) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return fmt.Errorf("list tickets: %w", err)
	}

	for _, t := range tickets {
		pr := obs.PRs[t.Ref()]
		if pr.State != plan.Merged {
			continue
		}
		exists, err := l.store.HasEvent(ctx, t.URL, store.EventPRMerged)
		if err != nil {
			return fmt.Errorf("check pr_merged event for %s: %w", t.URL, err)
		}
		if exists {
			continue
		}
		if err := l.store.AppendEvent(ctx, store.Event{
			At: pr.MergedAt, TicketURL: t.URL, Kind: store.EventPRMerged,
			Detail: fmt.Sprintf("PR #%d merged", pr.Number),
		}); err != nil {
			return fmt.Errorf("record pr_merged for %s: %w", t.URL, err)
		}
	}
	return nil
}
