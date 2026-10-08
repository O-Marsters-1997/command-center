package loop

import (
	"context"

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

	changed := false
	for _, t := range tickets {
		summary, ok := latest[t.URL]
		if !ok || !summary.HasOutcome || summary.Outcome != plan.OutcomePush {
			continue
		}
		pf := pushFacts[t.URL]
		if pf.Refused || pf.Failed || obs.PRs[plan.BranchKey(t.Repo, t.Branch)].State != plan.Open {
			continue
		}

		fact := &plan.RunFact{PROpen: true}
		l.cfg.PlanRules().ApplyVerdict(fact, t.Plan(), obs, vd)
		current := fact.Verdict.Label()
		if current == "" || lastVerdicts[t.URL] == current {
			continue
		}

		lastVerdicts[t.URL] = current
		changed = true
		if err := l.event(ctx, t.URL, store.EventVerdictTransition, current+": "+fact.Verdict.Reason); err != nil {
			return err
		}
	}
	if !changed {
		return nil
	}
	return l.store.SaveLastVerdicts(ctx, lastVerdicts)
}
