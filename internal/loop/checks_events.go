package loop

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

func (l *Loop) recordChecksLanded(ctx context.Context, prev, obs plan.Observation) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return fmt.Errorf("list tickets: %w", err)
	}

	for _, t := range tickets {
		key := plan.BranchKey(t.Repo, t.Branch)
		now := obs.PRs[key].Checks
		before := prev.PRs[key].Checks
		for _, name := range slices.Sorted(maps.Keys(now)) {
			landed := plan.ToVerdictCheckState(now[name])
			if landed == verdict.Pending {
				continue
			}
			if was, ok := before[name]; ok && plan.ToVerdictCheckState(was) != verdict.Pending {
				continue
			}
			if err := l.event(ctx, t.URL, store.EventChecksLanded, name+" "+landedWord(landed)); err != nil {
				return fmt.Errorf("record check %s for %s: %w", name, t.URL, err)
			}
		}
	}
	return nil
}

func landedWord(s verdict.CheckState) string {
	switch s {
	case verdict.Success:
		return "passed"
	case verdict.Skipped:
		return "skipped"
	default:
		return "failed"
	}
}
