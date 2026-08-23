package cc

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// retryPushVerb is the only verb push failed offers a human: the push step alone, no agent
// (docs/prds/prd-command-centre.md § The states).
const retryPushVerb = plan.VerbRetryPush

// pushContext is the per-tick facts pushOne needs that are the same for every candidate,
// computed once by pushPushable/applyRetryPushIntents rather than re-queried per task.
type pushContext struct {
	byURL      map[string]plan.Task
	stacking   map[string]bool
	prs        map[string]plan.PRState
	repoPaths  map[string]string
	denyByRepo map[string][]string
	obs        Observation
}

func (l *Loop) newPushContext(tasks []Task, obs Observation) pushContext {
	return pushContext{
		byURL:      planTasksByURL(tasks),
		stacking:   stackingByRepo(l.cfg.Repos),
		prs:        prsByBranch(obs),
		repoPaths:  repoPathsByName(l.ws.Root, l.cfg.Repos),
		denyByRepo: denyByRepo(l.cfg.Repos),
		obs:        obs,
	}
}

// pushPushable is job 1's push step (docs/prds/prd-command-centre.md § The tick): every task whose
// latest run disposed with commits gets its branch diffed against its base and either pushed
// and PR-opened, or refused outright. A push or PR-create failure is not retried automatically
// -- retry-push is your verb (see applyRetryPushIntents).
func (l *Loop) pushPushable(ctx context.Context, obs Observation) error {
	tasks, err := l.store.Tasks(ctx)
	if err != nil {
		return err
	}
	latest, err := l.store.LatestRunsByTask(ctx)
	if err != nil {
		return err
	}
	lastPushed, err := l.store.LastPushedTips(ctx)
	if err != nil {
		return err
	}
	pc := l.newPushContext(tasks, obs)

	byTicket := tasksByTicket(tasks)
	var candidates []plan.PushCandidate
	localTips := map[string]string{}
	for _, t := range tasks {
		summary, ok := latest[t.TicketURL]
		if !ok || !summary.HasOutcome || summary.Outcome != plan.OutcomePush {
			continue
		}
		// remove worktree (verbs.go) deletes the branch along with the worktree, and a task
		// that has ever run stays a push candidate forever (its latest run's outcome never
		// changes) -- without this guard, a removed task's absence from the same tick's own
		// worktree map would make every later tick's BranchTip fail on an unknown ref and
		// abort the whole tick.
		if pc.obs.Worktrees[t.Branch] == "" {
			continue
		}
		tip, err := BranchTip(ctx, pc.repoPaths[t.Repo], t.Branch)
		if err != nil {
			return fmt.Errorf("branch tip for %s: %w", t.TicketURL, err)
		}
		localTips[t.TicketURL] = tip
		candidates = append(candidates,
			plan.PushCandidate{TicketURL: t.TicketURL, LocalTip: tip, LastPushedTip: lastPushed[t.TicketURL]})
	}

	toPush := plan.PushPlan(candidates)
	if len(toPush) == 0 {
		return nil
	}

	facts, err := l.store.PushFacts(ctx)
	if err != nil {
		return err
	}

	now := l.now()
	for _, ticketURL := range toPush {
		if facts[ticketURL].Failed {
			continue // needs a human's retry-push, never an automatic one
		}
		if err := l.pushOne(ctx, byTicket[ticketURL], localTips[ticketURL], pc, now); err != nil {
			return err
		}
	}
	return nil
}

// applyRetryPushIntents consumes every pending retry-push request synchronously, bypassing
// pushPushable's failure gate: this is the retry (docs/prds/prd-command-centre.md § The states, push
// failed's only verb).
func (l *Loop) applyRetryPushIntents(ctx context.Context, obs Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, retryPushVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	tasks, err := l.store.Tasks(ctx)
	if err != nil {
		return err
	}
	byTicket := tasksByTicket(tasks)
	pc := l.newPushContext(tasks, obs)

	now := l.now()
	for _, intent := range intents {
		if t, ok := byTicket[intent.TaskID]; ok {
			tip, err := BranchTip(ctx, pc.repoPaths[t.Repo], t.Branch)
			if err != nil {
				return fmt.Errorf("branch tip for %s: %w", t.TicketURL, err)
			}
			if err := l.pushOne(ctx, t, tip, pc, now); err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// pushOne computes t's base fresh (the same pure plan.Unlocked call job 2 uses, over this tick's
// own snapshot), diffs the branch against it, and either records a refusal event or attempts the
// push-and-adopt-or-create sequence. An existing open PR is adopted, never duplicated (inv. 20):
// recording the push only after create-or-adopt is what makes a crash between them a non-event.
func (l *Loop) pushOne(ctx context.Context, t Task, localTip string, pc pushContext, now time.Time) error {
	unlock := plan.Unlocked(pc.byURL[t.TicketURL], pc.byURL, pc.prs, pc.stacking[t.Repo])
	if !unlock.Unlocked {
		return nil // its blocker's PR closed between run and push; nothing sane to diff against
	}
	base := unlock.BaseBranch
	repoPath := pc.repoPaths[t.Repo]

	changed, err := ChangedPaths(ctx, repoPath, "origin/"+base, t.Branch)
	if err != nil {
		return fmt.Errorf("diff %s against origin/%s: %w", t.Branch, base, err)
	}
	if refused, path := plan.PushRefused(changed, plan.Policy{Deny: pc.denyByRepo[t.Repo]}); refused {
		return l.store.AppendEvent(ctx, Event{At: now, TaskURL: t.TicketURL, Kind: eventPushRefused, Detail: path})
	}

	if err := Push(ctx, repoPath, t.Branch); err != nil {
		return l.store.AppendEvent(ctx, Event{At: now, TaskURL: t.TicketURL, Kind: eventPushFailed, Detail: err.Error()})
	}

	if pc.obs.PRs[t.Branch].State != gh.Open {
		body := plan.PRBody(base, pc.obs.PRs[base].Number)
		if err := gh.Create(ctx, pc.obs.Worktrees[t.Branch], base, body); err != nil {
			return l.store.AppendEvent(ctx,
				Event{At: now, TaskURL: t.TicketURL, Kind: eventPushFailed, Detail: err.Error()})
		}
	}

	baseSHA, err := RevParse(ctx, repoPath, "origin/"+base)
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", base, err)
	}
	if err := l.store.RecordPush(ctx, t.TicketURL, localTip, base, baseSHA, now); err != nil {
		return err
	}
	return l.store.AppendEvent(ctx, Event{
		At: now, TaskURL: t.TicketURL, Kind: eventPushed,
		Detail: fmt.Sprintf("pushed %s to origin/%s", localTip, base),
	})
}
