package cc

import (
	"context"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const (
	eventDraftReady       = "draft_ready"
	eventDraftReadyFailed = "draft_ready_failed"
)

// applyDraftGate un-drafts every open PR plan.DraftGate says is ready, and never re-drafts. A
// failed `gh pr ready` is never latched -- the next tick simply retries against a fresh
// observation (docs/designs/command-centre-design.md § 6 job 2, inv. 13).
func (l *Loop) applyDraftGate(ctx context.Context, obs Observation) error {
	tasks, err := l.store.Tasks(ctx)
	if err != nil {
		return err
	}
	byURL := planTasksByURL(tasks)
	prs := prsByBranch(obs)
	repoPaths := repoPathsByName(l.ws.Root, l.cfg.Repos)

	vd, err := verdictDepsFor(ctx, l.store, checksByRepo(l.cfg.Repos), mergifySHAByRepo(l.cfg.Repos))
	if err != nil {
		return err
	}

	now := l.now()
	for _, t := range tasks {
		pr := obs.PRs[t.Branch]
		if pr.State != gh.Open || !pr.IsDraft {
			continue
		}

		fact := &plan.RunFact{PROpen: true}
		applyVerdict(fact, t, obs, vd)
		gating := plan.GatingBlockers(byURL[t.TicketURL], byURL)
		if draft, _ := plan.DraftGate(gating, prs, fact.VerdictReviewMe); draft {
			continue
		}

		if err := l.readyOne(ctx, t, repoPaths[t.Repo], now); err != nil {
			return err
		}
	}
	return nil
}

// readyOne calls `gh pr ready` for one task, recording either outcome as an event: a failure is
// never latched, so leaving the row untouched here is exactly what lets the next tick retry.
func (l *Loop) readyOne(ctx context.Context, t Task, repoPath string, now time.Time) error {
	event := Event{At: now, TaskURL: t.TicketURL, Kind: eventDraftReady}
	if err := gh.Ready(ctx, repoPath, t.Branch); err != nil {
		event = Event{At: now, TaskURL: t.TicketURL, Kind: eventDraftReadyFailed, Detail: err.Error()}
	}
	return l.store.AppendEvent(ctx, event)
}
