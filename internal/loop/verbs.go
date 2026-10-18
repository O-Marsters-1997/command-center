package loop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/adapter/tp"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const heldWorktreeRefusal = plan.HeldWorktreeRefusal

func (l *Loop) applyAbortIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntent(ctx, snap, plan.VerbAbort, func(intent plan.Intent) error {
		e, ok := snap.Entry(intent.Ticket)
		if !ok {
			return nil
		}
		if err := l.abortOne(ctx, e.Ticket, obs); err != nil {
			return fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
		return nil
	})
}

func (l *Loop) abortOne(ctx context.Context, ticket plan.Ticket, obs plan.Observation) error {
	url := string(ticket.URL)
	worktreePath, refusal := idleWorktreeFor(ticket, obs)
	if refusal != "" {
		return l.event(ctx, url, store.EventMergeAbortFailed, refusal)
	}
	if err := git.MergeAbort(ctx, worktreePath); err != nil {
		return l.event(ctx, url, store.EventMergeAbortFailed, err.Error())
	}

	delete(obs.MidMerge, ticket.Ref())
	return l.event(ctx, url, store.EventMergeAborted, "")
}

func idleWorktreeFor(ticket plan.Ticket, obs plan.Observation) (worktreePath, refusal string) {
	worktreePath, ok := obs.Worktrees[ticket.Ref()]
	if !ok {
		return "", fmt.Sprintf("no worktree for %s", ticket.Branch)
	}
	if obs.Runs[ticket.URL].Alive {
		return "", fmt.Sprintf("a run is alive in %s", worktreePath)
	}
	return worktreePath, ""
}

func (l *Loop) applyFollowUpIntents(
	ctx context.Context, snap plan.Snapshot, t *tick, byURL map[plan.TicketURL]store.Ticket,
) error {
	return l.eachIntent(ctx, snap, plan.VerbFollowUp, func(intent plan.Intent) error {
		ticket, ok := l.ticket(byURL, string(intent.Ticket))
		if !ok {
			return nil
		}
		if err := l.followUpOne(ctx, snap, t, ticket, intent.Payload); err != nil {
			return fmt.Errorf("%s/%s: %w", ticket.Repo, ticket.Branch, err)
		}
		return nil
	})
}

func (l *Loop) followUpOne(
	ctx context.Context, snap plan.Snapshot, t *tick, ticket store.Ticket, payload string,
) error {
	picked := store.DecodeFollowUpPayload(payload)
	promptText := picked.Message
	url := plan.TicketURL(ticket.URL)
	if t.obs.Runs[url].Alive {
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

	launched, _ := snap.Entry(url)
	spec.choice = launched.Ticket.Choice
	if picked.Model != "" {
		spec.choice.Model = picked.Model
	}
	if picked.Effort != "" {
		spec.choice.Effort = option.Some(picked.Effort)
	}
	session, _ := snap.Session(url)
	sessionID, err := l.resumableSession(session)
	if err != nil {
		return fmt.Errorf("find resumable session: %w", err)
	}
	if sessionID != "" {
		entry, _ := snap.Entry(url)
		outcome := plan.OutcomeFailed
		if entry.Run.IsSome() {
			outcome = entry.Run.Unwrap().Outcome.UnwrapOr(plan.OutcomeFailed)
		}
		spec.prompt = plan.ComposeMessage(promptText, outcome)
		spec.resumeSessionID = sessionID
	}
	if err := l.spawnRun(ctx, t, spec); err != nil {
		return fmt.Errorf("spawn follow-up: %w", err)
	}
	if slices.Contains(t.spawned, url) {
		t.obs.MarkAlive(url)
	}
	return nil
}

func (l *Loop) resumableSession(sessionID string) (string, error) {
	if sessionID == "" {
		return "", nil
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
	return l.eachPendingIntent(ctx, plan.VerbCancel, func(intent plan.Intent) error {
		ticketID := string(intent.Ticket)
		members, err := l.store.CancelLaunchesFor(ctx, ticketID)
		if err != nil {
			return fmt.Errorf("cancel launches: %w", err)
		}
		detail := fmt.Sprintf("launch cancelled, %d member(s)", members)
		return l.event(ctx, ticketID, store.EventLaunchCancelled, detail)
	})
}

func (l *Loop) applyReRunIntents(
	ctx context.Context, snap plan.Snapshot, t *tick, byURL map[plan.TicketURL]store.Ticket,
) error {
	return l.eachIntent(ctx, snap, plan.VerbReRun, func(intent plan.Intent) error {
		ticket, ok := l.ticket(byURL, string(intent.Ticket))
		if !ok {
			return nil
		}
		entry, _ := snap.Entry(plan.TicketURL(ticket.URL))
		baseBranch := entry.Unlock.BaseBranch
		if baseBranch == "" {
			baseBranch = plan.DefaultBaseBranch
		}
		if err := l.reRunOne(ctx, t, ticket, entry, baseBranch); err != nil {
			return fmt.Errorf("%s/%s: %w", ticket.Repo, ticket.Branch, err)
		}
		return nil
	})
}

func (l *Loop) reRunOne(
	ctx context.Context, t *tick, ticket store.Ticket, entry plan.Entry, baseBranch string,
) error {
	launched, promptHash := entry.Ticket, entry.PromptHash
	begin := l.startOf(entry, t.obs)
	repoPath := l.checkout(ticket.Repo)
	worktreePath, ok := t.obs.Worktrees[ticket.Ref()]
	if !ok {
		if err := git.DeleteBranchIfExists(ctx, repoPath, ticket.Branch); err != nil {
			return fmt.Errorf("clear stale branch before re-cutting %s: %w", ticket.Branch, err)
		}
		if err := l.cutAndSpawn(ctx, t, ticket, launched, baseBranch, promptHash, begin); err != nil {
			return fmt.Errorf("re-cut %s: %w", ticket.Branch, err)
		}
		return nil
	}

	baselineSHA, err := git.BranchTip(ctx, repoPath, ticket.Branch)
	if err != nil {
		return fmt.Errorf("read baseline for re-run of %s: %w", ticket.URL, err)
	}
	if err := l.spawnRun(ctx, t, spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: baselineSHA, hash: promptHash,
		kind: plan.RunKindAgent, prompt: stepPrompt(ticket, launched, begin.step, nil), choice: launched.Choice,
		workflow: begin.workflow, step: begin.step, verify: begin.verify,
	}); err != nil {
		return fmt.Errorf("spawn re-run in %s: %w", worktreePath, err)
	}
	return nil
}

func (l *Loop) applyRemoveWorktreeIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntent(ctx, snap, plan.VerbRemoveWorktree, func(intent plan.Intent) error {
		e, ok := snap.Entry(intent.Ticket)
		if !ok {
			return nil
		}
		if err := l.removeWorktreeOne(ctx, e, snap.Removal(e.Ticket.URL), obs); err != nil {
			return fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
		return nil
	})
}

func (l *Loop) removeWorktreeOne(
	ctx context.Context, e plan.Entry, removal plan.Removal, obs plan.Observation,
) error {
	ticket := e.Ticket
	url := string(ticket.URL)
	refuse := func(detail string) error {
		return l.event(ctx, url, store.EventRemoveWorktreeRefused, detail)
	}

	if removal.Refusal != "" {
		return refuse(removal.Refusal)
	}

	merged := obs.PRs[ticket.Ref()].State == plan.Merged

	repoPath := l.checkout(ticket.Repo)
	worktreePath, worktreePresent := obs.Worktrees[ticket.Ref()]

	mode := tp.RemoveMerged
	if worktreePresent {
		dirty, err := git.Dirty(ctx, worktreePath)
		if err != nil {
			return fmt.Errorf("check worktree dirty for %s: %w", url, err)
		}
		if dirty {
			return refuse("worktree is dirty")
		}

		provenTips := make([]string, len(removal.ProvenTips))
		for i, tip := range removal.ProvenTips {
			provenTips[i] = string(tip)
		}
		state, err := git.RemovalStateFor(ctx, repoPath, ticket.Branch, provenTips...)
		if err != nil {
			return fmt.Errorf("check unpushed commits for %s: %w", url, err)
		}
		switch state {
		case git.RemovableByMerged:
		case git.RemovableByForce:
			mode = tp.RemoveForced
		case git.NotRemovable:
			return refuse("worktree holds unpushed commits")
		case git.RemovalUnknown:
			return fmt.Errorf("check unpushed commits for %s: removal state is unknown", url)
		}

		if err := l.worktrees.Remove(ctx, repoPath, ticket.Branch, mode); err != nil {
			return refuse(err.Error())
		}
	}

	if err := l.forge.CloseIssue(ctx, repoPath, url); err != nil {
		return refuse(err.Error())
	}

	var detail string
	if mode == tp.RemoveForced {
		detail = "forced: origin ref pruned, branch at a proven tip"
	}
	if err := l.event(ctx, url, store.EventWorktreeRemoved, detail); err != nil {
		return fmt.Errorf("record worktree removed: %w", err)
	}
	runs, err := l.store.WithdrawTicket(ctx, url, l.clock.Now(), merged)
	if err != nil {
		return fmt.Errorf("withdraw %s: %w", url, err)
	}
	l.pruneRunLogs(runs)
	return nil
}

func (l *Loop) pruneRunLogs(ids []plan.RunID) {
	for _, id := range ids {
		for _, ext := range []string{"jsonl", "prompt", "handoff.md", "system.md"} {
			path := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.%s", id, ext))
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				l.logger.Warn("remove run file", "path", path, "err", err)
			}
		}
	}
}
