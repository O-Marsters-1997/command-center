package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (l *Loop) recordVerdictTransitions(ctx context.Context, obs plan.Observation) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}
	latest, err := l.store.LatestRunsByTicket(ctx)
	if err != nil {
		return err
	}
	pushFacts, err := l.store.PushFacts(ctx)
	if err != nil {
		return err
	}
	lastVerdicts, err := l.store.LastVerdicts(ctx)
	if err != nil {
		return err
	}

	vd, err := l.store.VerdictFacts(ctx)
	if err != nil {
		return err
	}

	now := l.clock.Now()
	changed := false
	for _, t := range tickets {
		summary, ok := latest[t.URL]
		if !ok || !summary.HasOutcome || summary.Outcome != plan.OutcomePush {
			continue
		}
		pf := pushFacts[t.URL]
		if pf.Refused || pf.Failed || obs.PRs[branchKey(t.Repo, t.Branch)].State != plan.Open {
			continue
		}

		fact := &plan.RunFact{PROpen: true}
		l.cfg.PlanRules().ApplyVerdict(fact, t.Plan(), obs, vd)
		current := plan.VerdictLabel(fact)
		if current == "" || lastVerdicts[t.URL] == current {
			continue
		}

		lastVerdicts[t.URL] = current
		changed = true
		if err := l.store.AppendEvent(ctx, store.Event{
			At: now, TicketURL: t.URL, Kind: store.EventVerdictTransition,
			Detail: fmt.Sprintf("%s: %s", current, fact.VerdictReason),
		}); err != nil {
			return err
		}
	}
	if !changed {
		return nil
	}
	return l.store.SaveLastVerdicts(ctx, lastVerdicts)
}
