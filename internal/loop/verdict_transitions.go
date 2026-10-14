package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) recordVerdictTransitions(ctx context.Context, obs plan.Observation) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return fmt.Errorf("list tickets: %w", err)
	}
	latest, err := l.store.LatestRunsByTicket(ctx)
	if err != nil {
		return fmt.Errorf("latest runs: %w", err)
	}
	pushFacts, err := l.store.PushFacts(ctx)
	if err != nil {
		return fmt.Errorf("push facts: %w", err)
	}
	lastVerdicts, err := l.store.LastVerdicts(ctx)
	if err != nil {
		return fmt.Errorf("last verdicts: %w", err)
	}

	vd, err := l.store.VerdictFacts(ctx)
	if err != nil {
		return fmt.Errorf("verdict facts: %w", err)
	}

	changed := false
	for _, t := range tickets {
		summary, ok := latest[t.URL]
		if !ok || summary.Outcome != option.Some(plan.OutcomePush) {
			continue
		}
		pf := pushFacts[t.URL]
		if pf.Refused || pf.Failed || obs.PRs[plan.BranchKey(t.Repo, t.Branch)].State != plan.Open {
			continue
		}

		result := l.rules(obs).ApplyVerdict(t.Plan(), obs, vd)
		if result.IsNone() {
			continue
		}
		current := result.Unwrap().Kind.String()
		if current == "" || lastVerdicts[t.URL] == current {
			continue
		}

		lastVerdicts[t.URL] = current
		changed = true
		if err := l.event(ctx, t.URL, store.EventVerdictTransition, current+": "+result.Unwrap().Reason); err != nil {
			return fmt.Errorf("record verdict transition for %s: %w", t.URL, err)
		}
	}
	if !changed {
		return nil
	}
	if err := l.store.SaveLastVerdicts(ctx, lastVerdicts); err != nil {
		return fmt.Errorf("save last verdicts: %w", err)
	}
	return nil
}
