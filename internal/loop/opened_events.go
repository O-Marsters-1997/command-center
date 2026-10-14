package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) recordOpenedEvents(ctx context.Context, prev, obs plan.Observation) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return fmt.Errorf("list tickets: %w", err)
	}

	for _, t := range tickets {
		key := t.Ref()
		pr, observed := obs.PRs[key]
		if _, seen := prev.PRs[key]; !observed || seen {
			continue
		}
		exists, err := l.store.HasEvent(ctx, t.URL, store.EventPROpened)
		if err != nil {
			return fmt.Errorf("check pr_opened event for %s: %w", t.URL, err)
		}
		if exists {
			continue
		}
		detail := fmt.Sprintf("PR #%d https://github.com/%s/pull/%d", pr.Number, t.Repo, pr.Number)
		if err := l.event(ctx, t.URL, store.EventPROpened, detail); err != nil {
			return fmt.Errorf("record pr_opened for %s: %w", t.URL, err)
		}
	}
	return nil
}
