package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const eventPROpened = "pr_opened"

func (l *Loop) recordOpenedEvents(ctx context.Context, prev, obs plan.Observation) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}

	for _, t := range tickets {
		key := plan.BranchKey(t.Repo, t.Branch)
		pr, observed := obs.PRs[key]
		if _, seen := prev.PRs[key]; !observed || seen {
			continue
		}
		exists, err := l.store.HasEvent(ctx, t.URL, eventPROpened)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		detail := fmt.Sprintf("PR #%d https://github.com/%s/pull/%d", pr.Number, t.Repo, pr.Number)
		if err := l.event(ctx, t.URL, eventPROpened, detail); err != nil {
			return err
		}
	}
	return nil
}
