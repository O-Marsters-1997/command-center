package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	eventWorktreeRemoved  = "worktree_removed"
	eventLaunchCancelled  = "launch_cancelled"
	eventMergeAborted     = "merge_aborted"
	eventMergeAbortFailed = "merge_abort_failed"
	eventResolveRefused   = "resolve_refused"
	eventFollowUpRefused  = "follow_up_refused"
)

func (l *Loop) applyAbortIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntent(ctx, plan.VerbAbort, func(intent store.VerbIntent) error {
		e, ok := snap.Entry(intent.TicketID)
		if !ok {
			return nil
		}
		return l.abortOne(ctx, e.Ticket, obs)
	})
}

func (l *Loop) abortOne(ctx context.Context, ticket plan.Ticket, obs plan.Observation) error {
	worktreePath, refusal := idleWorktreeFor(ticket, obs)
	if refusal != "" {
		return l.event(ctx, ticket.URL, eventMergeAbortFailed, refusal)
	}
	if err := git.MergeAbort(ctx, worktreePath); err != nil {
		return l.event(ctx, ticket.URL, eventMergeAbortFailed, err.Error())
	}

	delete(obs.MidMerge, plan.BranchKey(ticket.Repo, ticket.Branch))
	return l.event(ctx, ticket.URL, eventMergeAborted, "")
}

func (l *Loop) applyResolveIntents(ctx context.Context, obs plan.Observation) error {
	return l.eachIntent(ctx, plan.VerbResolve, func(intent store.VerbIntent) error {
		ticket, ok, err := l.ticket(ctx, intent.TicketID)
		if err != nil || !ok {
			return err
		}
		return l.resolveOne(ctx, ticket, obs)
	})
}

func (l *Loop) resolveOne(ctx context.Context, ticket store.Ticket, obs plan.Observation) error {
	worktreePath, refusal := idleWorktreeFor(ticket.Plan(), obs)
	if refusal != "" {
		return l.event(ctx, ticket.URL, eventResolveRefused, refusal)
	}

	baselineSHA, err := git.BranchTip(ctx, l.checkout(ticket.Repo), ticket.Branch)
	if err != nil {
		return fmt.Errorf("read baseline for resolve of %s: %w", ticket.URL, err)
	}
	return l.spawnRun(ctx, spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: baselineSHA,
		kind: runKindResolve, prompt: plan.ComposeResolve(ticket.Plan()),
	})
}

func idleWorktreeFor(ticket plan.Ticket, obs plan.Observation) (worktreePath, refusal string) {
	worktreePath, ok := obs.Worktrees[plan.BranchKey(ticket.Repo, ticket.Branch)]
	if !ok {
		return "", fmt.Sprintf("no worktree for %s", ticket.Branch)
	}
	if obs.Runs[ticket.URL].Alive {
		return "", fmt.Sprintf("a run is alive in %s", worktreePath)
	}
	return worktreePath, ""
}

func (l *Loop) applyFollowUpIntents(ctx context.Context, obs plan.Observation) error {
	return l.eachIntent(ctx, plan.VerbFollowUp, func(intent store.VerbIntent) error {
		ticket, ok, err := l.ticket(ctx, intent.TicketID)
		if err != nil || !ok {
			return err
		}
		return l.followUpOne(ctx, ticket, intent.Payload, obs)
	})
}

func (l *Loop) followUpOne(ctx context.Context, ticket store.Ticket, promptText string, obs plan.Observation) error {
	worktreePath, refusal := idleWorktreeFor(ticket.Plan(), obs)
	if refusal != "" {
		return l.event(ctx, ticket.URL, eventFollowUpRefused, refusal)
	}

	baselineSHA, err := git.BranchTip(ctx, l.checkout(ticket.Repo), ticket.Branch)
	if err != nil {
		return fmt.Errorf("read baseline for follow-up of %s: %w", ticket.URL, err)
	}
	return l.spawnRun(ctx, spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: baselineSHA,
		kind: runKindFollowUp, prompt: plan.ComposeFollowUp(promptText),
	})
}

func (l *Loop) applyCancelIntents(ctx context.Context) error {
	return l.eachIntent(ctx, plan.VerbCancel, func(intent store.VerbIntent) error {
		members, err := l.store.CancelLaunchesFor(ctx, intent.TicketID)
		if err != nil {
			return err
		}
		return l.event(ctx, intent.TicketID, eventLaunchCancelled, fmt.Sprintf("launch cancelled, %d member(s)", members))
	})
}

func (l *Loop) applyReRunIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntent(ctx, plan.VerbReRun, func(intent store.VerbIntent) error {
		ticket, ok, err := l.ticket(ctx, intent.TicketID)
		if err != nil || !ok {
			return err
		}
		entry, _ := snap.Entry(ticket.URL)
		baseBranch := entry.Unlock.BaseBranch
		if baseBranch == "" {
			baseBranch = plan.DefaultBaseBranch
		}
		return l.reRunOne(ctx, ticket, baseBranch, obs, entry.PromptHash)
	})
}

func (l *Loop) reRunOne(
	ctx context.Context, ticket store.Ticket, baseBranch string, obs plan.Observation, promptHash string,
) error {
	repoPath := l.checkout(ticket.Repo)
	worktreePath, ok := obs.Worktrees[plan.BranchKey(ticket.Repo, ticket.Branch)]
	if !ok {
		if err := git.DeleteBranchIfExists(ctx, repoPath, ticket.Branch); err != nil {
			return fmt.Errorf("clear stale branch before re-cutting %s: %w", ticket.Branch, err)
		}
		return l.cutAndSpawn(ctx, ticket, baseBranch, promptHash)
	}

	baselineSHA, err := git.BranchTip(ctx, repoPath, ticket.Branch)
	if err != nil {
		return fmt.Errorf("read baseline for re-run of %s: %w", ticket.URL, err)
	}
	return l.spawnRun(ctx, spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: baselineSHA, hash: promptHash,
		kind: runKindAgent, prompt: agentPrompt(ticket),
	})
}

func (l *Loop) applyRemoveWorktreeIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntent(ctx, plan.VerbRemoveWorktree, func(intent store.VerbIntent) error {
		e, ok := snap.Entry(intent.TicketID)
		if !ok {
			return nil
		}
		lastPushed, err := l.store.LastPushedTips(ctx)
		if err != nil {
			return err
		}
		return l.removeWorktreeOne(ctx, e, obs, lastPushed[e.Ticket.URL])
	})
}

func (l *Loop) removeWorktreeOne(ctx context.Context, e plan.Entry, obs plan.Observation, lastPushed string) error {
	ticket := e.Ticket
	refuse := func(detail string) error {
		return l.event(ctx, ticket.URL, store.EventRemoveWorktreeRefused, detail)
	}

	merged := obs.PRs[plan.BranchKey(ticket.Repo, ticket.Branch)].State == plan.Merged
	baseGone := e.Run != nil && e.Unlock.BlockerClosed
	if !merged && !baseGone {
		return refuse("neither merged nor base gone")
	}

	repoPath := l.checkout(ticket.Repo)
	worktreePath, worktreePresent := obs.Worktrees[plan.BranchKey(ticket.Repo, ticket.Branch)]

	mode := git.RemoveMerged
	if worktreePresent {
		dirty, err := git.Dirty(ctx, worktreePath)
		if err != nil {
			return fmt.Errorf("check worktree dirty for %s: %w", ticket.URL, err)
		}
		if dirty {
			return refuse("worktree is dirty")
		}

		state, err := git.RemovalStateFor(ctx, repoPath, ticket.Branch, lastPushed)
		if err != nil {
			return fmt.Errorf("check unpushed commits for %s: %w", ticket.URL, err)
		}
		if state == git.NotRemovable {
			return refuse("worktree holds unpushed commits")
		}
		if state == git.RemovableByForce {
			mode = git.RemoveForced
		}

		if err := l.worktrees.Remove(ctx, repoPath, ticket.Branch, mode); err != nil {
			return refuse(err.Error())
		}
	}

	if err := l.forge.CloseIssue(ctx, repoPath, ticket.URL); err != nil {
		return refuse(err.Error())
	}

	if err := l.pruneRunLogs(ctx, ticket.URL); err != nil {
		return err
	}
	var detail string
	if mode == git.RemoveForced {
		detail = "forced: origin ref pruned, branch at last pushed tip"
	}
	if err := l.event(ctx, ticket.URL, eventWorktreeRemoved, detail); err != nil {
		return err
	}
	return l.store.WithdrawTicket(ctx, ticket.URL, l.clock.Now(), merged)
}

func (l *Loop) pruneRunLogs(ctx context.Context, ticketID string) error {
	ids, err := l.store.RunIDsForTicket(ctx, ticketID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		_ = os.Remove(filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.jsonl", id)))
		_ = os.Remove(filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.prompt", id)))
	}
	return nil
}
