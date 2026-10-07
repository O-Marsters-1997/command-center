package loop

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// recordVerdictTransitions logs one event per ticket whose CI verdict label ("checking",
// "review_me", "needs_you", "ci_failed", "base_moved" or "waiting_on_producer_deploy") differs
// from what the previous tick recorded -- the last category of what `events` needs to reconstruct
// the whole run
// (docs/prds/prd-command-centre.md § Phase 6).
// It computes the verdict the exact way the page does (plan.Rules.ApplyVerdict), over this same
// tick's observation, so a transition an operator would see on the next page load is exactly
// the transition logged here.
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
