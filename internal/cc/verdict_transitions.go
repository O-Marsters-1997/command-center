package cc

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const eventVerdictTransition = "verdict_transition"

// recordVerdictTransitions logs one event per task whose CI verdict label ("checking",
// "review_me", "needs_you", "base_moved" or "waiting_on_producer_deploy") differs from what the
// previous tick recorded -- the last category of what `events` needs to reconstruct the whole run
// (docs/prds/prd-command-centre.md § Phase 6).
// It computes the verdict the exact way the page does (applyVerdict, server.go), over this same
// tick's observation, so a transition an operator would see on the next page load is exactly
// the transition logged here.
func (l *Loop) recordVerdictTransitions(ctx context.Context, obs Observation) error {
	tasks, err := l.store.Tasks(ctx)
	if err != nil {
		return err
	}
	latest, err := l.store.LatestRunsByTask(ctx)
	if err != nil {
		return err
	}
	pushFacts, err := l.store.PushFacts(ctx)
	if err != nil {
		return err
	}
	pushRows, err := l.store.LatestPushes(ctx)
	if err != nil {
		return err
	}
	checkingTicks, err := l.store.CheckingTicks(ctx)
	if err != nil {
		return err
	}
	lastVerdicts, err := l.store.LastVerdicts(ctx)
	if err != nil {
		return err
	}

	vd := verdictDeps{
		pushRows: pushRows, checkingTicks: checkingTicks,
		checksByRepo: checksByRepo(l.cfg.Repos), mergifySHAByRepo: mergifySHAByRepo(l.cfg.Repos),
		compatCheckByRepo: compatCheckByRepo(l.cfg.Repos),
	}

	now := l.now()
	changed := false
	for _, t := range tasks {
		summary, ok := latest[t.TicketURL]
		if !ok || !summary.HasOutcome || summary.Outcome != plan.OutcomePush {
			continue
		}
		pf := pushFacts[t.TicketURL]
		if pf.Refused || pf.Failed || obs.PRs[t.Branch].State != gh.Open {
			continue
		}

		fact := &plan.RunFact{PROpen: true}
		applyVerdict(fact, t, obs, vd)
		current := verdictLabel(fact)
		if current == "" || lastVerdicts[t.TicketURL] == current {
			continue
		}

		lastVerdicts[t.TicketURL] = current
		changed = true
		if err := l.store.AppendEvent(ctx, Event{
			At: now, TaskURL: t.TicketURL, Kind: eventVerdictTransition,
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

// verdictLabel names a task's just-computed verdict for comparison against the last recorded
// one, and (server.go's derive) a row's own label as its dependents' BaseVerdict. Empty for a nil
// fact (no run yet) or when applyVerdict left every flag untouched -- no predicate configured for
// this repo (§7) -- neither of which may count as a transition.
func verdictLabel(fact *plan.RunFact) string {
	if fact == nil {
		return ""
	}
	switch {
	case fact.VerdictBaseMoved:
		return "base_moved"
	case fact.VerdictWaitingOnProducer:
		return "waiting_on_producer_deploy"
	case fact.VerdictReviewMe:
		return "review_me"
	case fact.VerdictNeedsYou:
		return "needs_you"
	case fact.VerdictReason != "":
		return "checking"
	default:
		return ""
	}
}
