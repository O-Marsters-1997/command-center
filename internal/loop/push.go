package loop

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const retryPushVerb = plan.VerbRetryPush

const commitResolutionVerb = plan.VerbCommitResolution

const eventCommitResolutionRefused = "commit_resolution_refused"

func (l *Loop) pushPushable(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	lastPushed, err := l.store.LastPushedTips(ctx)
	if err != nil {
		return err
	}

	var candidates []plan.PushCandidate
	for _, e := range snap.Entries {
		if e.Run == nil || !e.Run.HasOutcome || e.Run.Outcome != plan.OutcomePush {
			continue
		}
		tip, ok := obs.LocalTips[branchKey(e.Ticket.Repo, e.Ticket.Branch)]
		if !ok || obs.Runs[e.Ticket.URL].Alive {
			continue
		}
		candidates = append(candidates,
			plan.PushCandidate{URL: e.Ticket.URL, LocalTip: tip, LastPushedTip: lastPushed[e.Ticket.URL]})
	}

	toPush := plan.PushPlan(candidates)
	if len(toPush) == 0 {
		return nil
	}

	facts, err := l.store.PushFacts(ctx)
	if err != nil {
		return err
	}
	refreshFacts, err := l.store.RefreshFacts(ctx)
	if err != nil {
		return err
	}

	now := l.clock.Now()
	for _, ticketURL := range toPush {
		if facts[ticketURL].Failed || facts[ticketURL].Refused || refreshFacts[ticketURL].VerificationFailed {
			continue
		}
		e, _ := snap.Entry(ticketURL)
		tip := obs.LocalTips[branchKey(e.Ticket.Repo, e.Ticket.Branch)]
		if err := l.pushOne(ctx, e, tip, obs, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) applyRetryPushIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, retryPushVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	now := l.clock.Now()
	for _, intent := range intents {
		if e, ok := snap.Entry(intent.TicketID); ok {
			if tip, ok := obs.LocalTips[branchKey(e.Ticket.Repo, e.Ticket.Branch)]; ok {
				if err := l.pushOne(ctx, e, tip, obs, now); err != nil {
					return err
				}
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) applyCommitResolutionIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, commitResolutionVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	now := l.clock.Now()
	for _, intent := range intents {
		if e, ok := snap.Entry(intent.TicketID); ok {
			if err := l.commitResolutionOne(ctx, e, obs, now); err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) commitResolutionOne(ctx context.Context, e plan.Entry, obs plan.Observation, now time.Time) error {
	ticket := e.Ticket
	refuse := func(detail string) error {
		return l.store.AppendEvent(ctx,
			store.Event{At: now, TicketURL: ticket.URL, Kind: eventCommitResolutionRefused, Detail: detail})
	}

	worktreePath, ok := obs.Worktrees[branchKey(ticket.Repo, ticket.Branch)]
	if !ok {
		return refuse(fmt.Sprintf("no worktree for %s", ticket.Branch))
	}
	if obs.Runs[ticket.URL].Alive {
		return refuse(fmt.Sprintf("a run is alive in %s", worktreePath))
	}

	unmerged, err := git.UnmergedPaths(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("read unmerged paths for %s: %w", ticket.URL, err)
	}
	if len(unmerged) > 0 {
		return refuse(fmt.Sprintf("still unmerged: %s", strings.Join(unmerged, ", ")))
	}

	midMerge, err := git.MidMerge(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("read merge state for %s: %w", ticket.URL, err)
	}
	if midMerge {
		staged, err := git.StagedPaths(ctx, worktreePath)
		if err != nil {
			return fmt.Errorf("read staged paths for %s: %w", ticket.URL, err)
		}
		if len(staged) == 0 {
			return refuse("nothing staged to commit")
		}
		if err := git.CommitNoEdit(ctx, worktreePath); err != nil {
			return refuse(err.Error())
		}
	}

	tip, err := git.BranchTip(ctx, repoPathsByName(l.cfg.Repos)[ticket.Repo], ticket.Branch)
	if err != nil {
		return fmt.Errorf("read tip after commit resolution for %s: %w", ticket.URL, err)
	}
	return l.pushOne(ctx, e, tip, obs, now)
}

func pushBranch(ctx context.Context, repoPath, branch, recordedTip string, restacked bool) error {
	if !restacked || recordedTip == "" {
		return git.Push(ctx, repoPath, branch)
	}
	descended, err := git.Ancestor(ctx, repoPath, recordedTip, branch)
	if err != nil {
		return err
	}
	if descended {
		return git.Push(ctx, repoPath, branch)
	}
	return git.PushRestacked(ctx, repoPath, branch, recordedTip)
}

func (l *Loop) pushOne(
	ctx context.Context, e plan.Entry, localTip string, obs plan.Observation, now time.Time,
) error {
	t := e.Ticket
	if !e.Unlock.Unlocked {
		return nil
	}
	base := e.Unlock.BaseBranch
	repoPath := repoPathsByName(l.cfg.Repos)[t.Repo]

	changed, err := git.ChangedPaths(ctx, repoPath, "origin/"+base, t.Branch)
	if err != nil {
		return fmt.Errorf("diff %s against origin/%s: %w", t.Branch, base, err)
	}
	if refused, path := plan.PushRefused(changed, plan.Policy{Deny: l.cfg.PlanRules().Deny[t.Repo]}); refused {
		return l.store.AppendEvent(ctx, store.Event{At: now, TicketURL: t.URL, Kind: store.EventPushRefused, Detail: path})
	}

	pushedTips, err := l.store.LastPushedTips(ctx)
	if err != nil {
		return err
	}
	restacked, err := l.store.RestackedSinceLastPush(ctx)
	if err != nil {
		return err
	}
	if err := pushBranch(ctx, repoPath, t.Branch, pushedTips[t.URL], restacked[t.URL]); err != nil {
		return l.store.AppendEvent(ctx, store.Event{
			At: now, TicketURL: t.URL, Kind: store.EventPushFailed, Detail: err.Error(),
		})
	}

	if obs.PRs[branchKey(t.Repo, t.Branch)].State != plan.Open {
		body := plan.PRBody(base, obs.PRs[branchKey(t.Repo, base)].Number)
		if err := l.forge.Create(ctx, obs.Worktrees[branchKey(t.Repo, t.Branch)], base, body, e.OpensAsDraft); err != nil {
			return l.store.AppendEvent(ctx,
				store.Event{At: now, TicketURL: t.URL, Kind: store.EventPushFailed, Detail: err.Error()})
		}
	}

	baseSHA, err := git.RevParse(ctx, repoPath, "origin/"+base)
	if err != nil {
		return fmt.Errorf("resolve origin/%s: %w", base, err)
	}
	if err := l.store.RecordPush(ctx, t.URL, localTip, base, baseSHA, now); err != nil {
		return err
	}
	return l.store.AppendEvent(ctx, store.Event{
		At: now, TicketURL: t.URL, Kind: store.EventPushed,
		Detail: fmt.Sprintf("pushed %s to origin/%s", localTip, base),
	})
}
