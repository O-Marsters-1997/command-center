package loop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tp"
)

const heldWorktreeRefusal = "the agent is awaiting an answer; its worktree stays as it left it"

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
		return l.event(ctx, ticket.URL, store.EventMergeAbortFailed, refusal)
	}
	if err := git.MergeAbort(ctx, worktreePath); err != nil {
		return l.event(ctx, ticket.URL, store.EventMergeAbortFailed, err.Error())
	}

	delete(obs.MidMerge, plan.BranchKey(ticket.Repo, ticket.Branch))
	return l.event(ctx, ticket.URL, store.EventMergeAborted, "")
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

func (l *Loop) applyFollowUpIntents(ctx context.Context, t *tick) error {
	return l.eachIntent(ctx, plan.VerbFollowUp, func(intent store.VerbIntent) error {
		ticket, ok, err := l.ticket(ctx, intent.TicketID)
		if err != nil || !ok {
			return err
		}
		return l.followUpOne(ctx, t, ticket, intent.Payload)
	})
}

func (l *Loop) followUpOne(ctx context.Context, t *tick, ticket store.Ticket, promptText string) error {
	if t.obs.Runs[ticket.URL].Alive {
		return errIntentPending
	}
	worktreePath, refusal := idleWorktreeFor(ticket.Plan(), t.obs)
	if refusal != "" {
		return l.event(ctx, ticket.URL, store.EventFollowUpRefused, refusal)
	}

	baselineSHA, err := git.BranchTip(ctx, l.checkout(ticket.Repo), ticket.Branch)
	if err != nil {
		return fmt.Errorf("read baseline for follow-up of %s: %w", ticket.URL, err)
	}
	spec := spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: baselineSHA,
		kind: plan.RunKindFollowUp, prompt: plan.ComposeFollowUp(promptText), message: promptText,
	}

	sessionID, err := l.resumableSession(ctx, ticket.URL)
	if err != nil {
		return err
	}
	if sessionID != "" {
		latest, err := l.store.LatestRunsByTicket(ctx)
		if err != nil {
			return err
		}
		spec.prompt = plan.ComposeMessage(promptText, latest[ticket.URL].Outcome)
		spec.resumeSessionID = sessionID
	}
	if err := l.spawnRun(ctx, t, spec); err != nil {
		return err
	}
	if slices.Contains(t.spawned, ticket.URL) {
		t.obs.Runs[ticket.URL] = plan.RunObservation{Alive: true}
	}
	return nil
}

func (l *Loop) resumableSession(ctx context.Context, ticketURL string) (string, error) {
	sessionID, err := l.store.LatestSessionID(ctx, ticketURL)
	if err != nil || sessionID == "" {
		return "", err
	}
	projects, err := os.ReadDir(l.cfg.ClaudeProjectsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read claude projects dir: %w", err)
	}
	for _, project := range projects {
		transcript := filepath.Join(l.cfg.ClaudeProjectsDir, project.Name(), sessionID+".jsonl")
		if _, err := os.Stat(transcript); err == nil {
			return sessionID, nil
		}
	}
	return "", nil
}

func (l *Loop) applyCancelIntents(ctx context.Context) error {
	return l.eachIntent(ctx, plan.VerbCancel, func(intent store.VerbIntent) error {
		members, err := l.store.CancelLaunchesFor(ctx, intent.TicketID)
		if err != nil {
			return err
		}
		detail := fmt.Sprintf("launch cancelled, %d member(s)", members)
		return l.event(ctx, intent.TicketID, store.EventLaunchCancelled, detail)
	})
}

func (l *Loop) applyReRunIntents(ctx context.Context, snap plan.Snapshot, t *tick) error {
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
		return l.reRunOne(ctx, t, ticket, baseBranch, entry.PromptHash)
	})
}

func (l *Loop) reRunOne(
	ctx context.Context, t *tick, ticket store.Ticket, baseBranch string, promptHash string,
) error {
	repoPath := l.checkout(ticket.Repo)
	worktreePath, ok := t.obs.Worktrees[plan.BranchKey(ticket.Repo, ticket.Branch)]
	if !ok {
		if err := git.DeleteBranchIfExists(ctx, repoPath, ticket.Branch); err != nil {
			return fmt.Errorf("clear stale branch before re-cutting %s: %w", ticket.Branch, err)
		}
		return l.cutAndSpawn(ctx, t, ticket, baseBranch, promptHash)
	}

	baselineSHA, err := git.BranchTip(ctx, repoPath, ticket.Branch)
	if err != nil {
		return fmt.Errorf("read baseline for re-run of %s: %w", ticket.URL, err)
	}
	return l.spawnRun(ctx, t, spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: baselineSHA, hash: promptHash,
		kind: plan.RunKindAgent, prompt: agentPrompt(ticket),
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

	if e.HoldsWorktree() {
		return refuse(heldWorktreeRefusal)
	}

	pr := obs.PRs[plan.BranchKey(ticket.Repo, ticket.Branch)]
	merged := pr.State == plan.Merged
	baseGone := e.Run != nil && e.Unlock.BlockerClosed
	if !merged && !baseGone {
		return refuse("neither merged nor base gone")
	}

	repoPath := l.checkout(ticket.Repo)
	worktreePath, worktreePresent := obs.Worktrees[plan.BranchKey(ticket.Repo, ticket.Branch)]

	mode := tp.RemoveMerged
	if worktreePresent {
		dirty, err := git.Dirty(ctx, worktreePath)
		if err != nil {
			return fmt.Errorf("check worktree dirty for %s: %w", ticket.URL, err)
		}
		if dirty {
			return refuse("worktree is dirty")
		}

		provenTips := []string{lastPushed}
		if merged && pr.HeadOid != "" {
			provenTips = append(provenTips, pr.HeadOid)
		}
		state, err := git.RemovalStateFor(ctx, repoPath, ticket.Branch, provenTips...)
		if err != nil {
			return fmt.Errorf("check unpushed commits for %s: %w", ticket.URL, err)
		}
		switch state {
		case git.RemovableByMerged:
		case git.RemovableByForce:
			mode = tp.RemoveForced
		case git.NotRemovable:
			return refuse("worktree holds unpushed commits")
		case git.RemovalUnknown:
			return fmt.Errorf("check unpushed commits for %s: removal state is unknown", ticket.URL)
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
	if mode == tp.RemoveForced {
		detail = "forced: origin ref pruned, branch at a proven tip"
	}
	if err := l.event(ctx, ticket.URL, store.EventWorktreeRemoved, detail); err != nil {
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
		for _, ext := range []string{"jsonl", "prompt"} {
			path := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.%s", id, ext))
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				l.logger.Warn("remove run file", "path", path, "err", err)
			}
		}
	}
	return nil
}
