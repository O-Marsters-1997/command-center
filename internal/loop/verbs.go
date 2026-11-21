package loop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/O-Marsters-1997/treepad"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const heldWorktreeRefusal = plan.HeldWorktreeRefusal

func (l *Loop) applyAbortIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntent(ctx, snap, plan.VerbAbort, func(intent plan.Intent) (handled, error) {
		e, ok := snap.Entry(intent.Ticket)
		if !ok {
			return failedWith(untrackedTicket), nil
		}
		result, err := l.abortOne(ctx, e.Ticket, obs)
		if err != nil {
			return handled{}, fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
		return result, nil
	})
}

func (l *Loop) abortOne(ctx context.Context, ticket plan.Ticket, obs plan.Observation) (handled, error) {
	url := string(ticket.URL)
	worktreePath, refusal := idleWorktreeFor(ticket, obs)
	if refusal != "" {
		return l.refuse(ctx, url, store.EventMergeAbortFailed, refusal)
	}
	if err := git.MergeAbort(ctx, worktreePath); err != nil {
		return l.refuse(ctx, url, store.EventMergeAbortFailed, err.Error())
	}

	delete(obs.MidMerge, ticket.Ref())
	return succeeded, l.event(ctx, url, store.EventMergeAborted, "")
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
	return l.eachIntent(ctx, snap, plan.VerbFollowUp, func(intent plan.Intent) (handled, error) {
		ticket, ok := l.ticket(byURL, string(intent.Ticket))
		if !ok {
			return failedWith(untrackedTicket), nil
		}
		picked := store.DecodeFollowUpPayload(intent.Payload)
		result, err := l.followUpOne(ctx, snap, t, ticket, picked, plan.CauseNone)
		if err != nil {
			return handled{}, fmt.Errorf("%s/%s: %w", ticket.Repo, ticket.Branch, err)
		}
		return result, nil
	})
}

func (l *Loop) followUpOne(
	ctx context.Context, snap plan.Snapshot, t *tick, ticket store.Ticket,
	picked store.FollowUpPayload, cause plan.FollowUpCause,
) (handled, error) {
	promptText := picked.Message
	url := plan.TicketURL(ticket.URL)
	if t.obs.Runs[url].Alive {
		return handled{}, errIntentPending
	}
	worktreePath, refusal := idleWorktreeFor(ticket.Plan(), t.obs)
	if refusal != "" {
		return l.refuse(ctx, ticket.URL, store.EventFollowUpRefused, refusal)
	}

	baselineSHA, err := git.BranchTip(ctx, l.checkout(ticket.Repo), ticket.Branch)
	if err != nil {
		return handled{}, fmt.Errorf("read baseline for follow-up of %s: %w", ticket.URL, err)
	}
	spec := spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: baselineSHA,
		kind: plan.RunKindFollowUp, prompt: plan.ComposeFollowUp(promptText), message: promptText, cause: cause,
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
		return handled{}, fmt.Errorf("find resumable session: %w", err)
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
	if launched.State == plan.CIFailed || launched.State == plan.Parked {
		logs, err := l.failedLogs(ctx, ticket, launched, t.obs)
		if err != nil {
			return handled{}, err
		}
		spec.prompt += logs
	}
	runID, err := l.spawnRun(ctx, t, spec)
	if err != nil {
		return handled{}, fmt.Errorf("spawn follow-up: %w", err)
	}
	if slices.Contains(t.spawned, url) {
		t.obs.MarkAlive(url)
	}
	return spawned(runID), nil
}

func (l *Loop) failedLogs(
	ctx context.Context, ticket store.Ticket, entry plan.Entry, obs plan.Observation,
) (string, error) {
	if entry.Run.IsNone() || entry.Run.Unwrap().Verdict.IsNone() {
		return "", nil
	}
	checks := obs.PRs[ticket.Ref()].Checks
	var b strings.Builder
	for _, name := range entry.Run.Unwrap().Verdict.Unwrap().RedLeaves {
		b.WriteString("\n\n")
		log, err := l.forge.FailedLog(ctx, l.checkout(ticket.Repo), checks[name].DetailsURL)
		if err != nil {
			b.WriteString(plan.ComposeCILogUnavailable(name))
			if eventErr := l.event(ctx, ticket.URL, store.EventCILogUnavailable, name+": "+err.Error()); eventErr != nil {
				return "", eventErr
			}
			continue
		}
		b.WriteString(plan.ComposeCILog(name, log))
	}
	return b.String(), nil
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
	return l.eachPendingIntent(ctx, plan.VerbCancel, func(intent plan.Intent) (handled, error) {
		ticketID := string(intent.Ticket)
		switch err := l.store.DisarmTicket(ctx, ticketID); {
		case errors.Is(err, store.ErrArmedDependent):
			return l.refuse(ctx, ticketID, store.EventLaunchCancelled, "disarm refused: "+err.Error())
		case err != nil:
			return handled{}, fmt.Errorf("disarm ticket: %w", err)
		}
		return succeeded, l.event(ctx, ticketID, store.EventLaunchCancelled, "ticket disarmed")
	})
}

func (l *Loop) applyReRunIntents(
	ctx context.Context, snap plan.Snapshot, t *tick, byURL map[plan.TicketURL]store.Ticket,
) error {
	return l.eachIntent(ctx, snap, plan.VerbReRun, func(intent plan.Intent) (handled, error) {
		ticket, ok := l.ticket(byURL, string(intent.Ticket))
		if !ok {
			return failedWith(untrackedTicket), nil
		}
		entry, _ := snap.Entry(plan.TicketURL(ticket.URL))
		baseBranch := entry.Unlock.BaseBranch
		if baseBranch == "" {
			baseBranch = plan.DefaultBaseBranch
		}
		runID, err := l.reRunOne(ctx, t, ticket, entry, baseBranch)
		if err != nil {
			return handled{}, fmt.Errorf("%s/%s: %w", ticket.Repo, ticket.Branch, err)
		}
		return spawned(runID), nil
	})
}

func (l *Loop) reRunOne(
	ctx context.Context, t *tick, ticket store.Ticket, entry plan.Entry, baseBranch string,
) (plan.RunID, error) {
	launched, promptHash := entry.Ticket, entry.PromptHash
	begin := l.startOf(entry, t.obs)
	repoPath := l.checkout(ticket.Repo)
	worktreePath, ok := t.obs.Worktrees[ticket.Ref()]
	if !ok {
		if err := git.DeleteBranchIfExists(ctx, repoPath, ticket.Branch); err != nil {
			return 0, fmt.Errorf("clear stale branch before re-cutting %s: %w", ticket.Branch, err)
		}
		runID, err := l.cutAndSpawn(ctx, t, ticket, launched, baseBranch, promptHash, begin)
		if err != nil {
			return 0, fmt.Errorf("re-cut %s: %w", ticket.Branch, err)
		}
		return runID, nil
	}

	baselineSHA, err := git.BranchTip(ctx, repoPath, ticket.Branch)
	if err != nil {
		return 0, fmt.Errorf("read baseline for re-run of %s: %w", ticket.URL, err)
	}
	runID, err := l.spawnRun(ctx, t, spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: baselineSHA, hash: promptHash,
		kind: plan.RunKindAgent, prompt: stepPrompt(ticket, launched, begin.step, nil), choice: launched.Choice,
		workflow: begin.workflow, step: begin.step, verify: begin.verify,
	})
	if err != nil {
		return 0, fmt.Errorf("spawn re-run in %s: %w", worktreePath, err)
	}
	return runID, nil
}

func (l *Loop) applyRemoveWorktreeIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	seen := map[plan.TicketURL]bool{}
	return l.eachIntent(ctx, snap, plan.VerbRemoveWorktree, func(intent plan.Intent) (handled, error) {
		e, ok := snap.Entry(intent.Ticket)
		if !ok {
			return failedWith(untrackedTicket), nil
		}
		if seen[intent.Ticket] {
			return succeeded, nil
		}
		seen[intent.Ticket] = true
		result, err := l.removeWorktreeOne(ctx, e, snap.Removal(e.Ticket.URL), obs)
		if err != nil {
			return handled{}, fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
		return result, nil
	})
}

func (l *Loop) queueAutoRemovals(ctx context.Context, snap plan.Snapshot) error {
	for _, url := range snap.AutoRemovals() {
		request := store.VerbRequest{
			Ticket: string(url), Verb: plan.VerbRemoveWorktree, At: l.clock.Now(), QueuedBy: plan.QueuedByLoop,
		}
		if _, err := l.store.QueueVerbIntent(ctx, request); err != nil {
			return fmt.Errorf("queue removal of %s: %w", url, err)
		}
	}
	return nil
}

func (l *Loop) removeWorktreeOne(
	ctx context.Context, e plan.Entry, removal plan.Removal, obs plan.Observation,
) (handled, error) {
	ticket := e.Ticket
	url := string(ticket.URL)
	refuse := func(detail string) (handled, error) {
		return l.refuse(ctx, url, store.EventRemoveWorktreeRefused, detail)
	}

	if removal.Refusal != "" {
		return refuse(removal.Refusal)
	}

	merged := obs.PRs[ticket.Ref()].State == plan.Merged

	repoPath := l.checkout(ticket.Repo)
	_, worktreePresent := obs.Worktrees[ticket.Ref()]

	forced := false
	if worktreePresent {
		provenTips := make([]string, len(removal.ProvenTips))
		for i, tip := range removal.ProvenTips {
			provenTips[i] = string(tip)
		}
		state, err := git.RemovalStateFor(ctx, repoPath, ticket.Branch, provenTips...)
		if err != nil {
			return handled{}, fmt.Errorf("check unpushed commits for %s: %w", url, err)
		}
		switch state {
		case git.RemovableByMerged:
		case git.RemovableByForce:
			forced = true
		case git.NotRemovable:
			return refuse("worktree holds unpushed commits")
		case git.RemovalUnknown:
			return handled{}, fmt.Errorf("check unpushed commits for %s: removal state is unknown", url)
		}

		err = l.worktrees.Remove(ctx, repoPath, ticket.Branch)
		switch {
		case errors.Is(err, treepad.ErrPostHook):
			l.logger.Warn("post hook failed after removing worktree", "ticket", url, "err", err)
		case errors.Is(err, treepad.ErrDirty):
			return refuse("worktree is dirty")
		case err != nil:
			return refuse(err.Error())
		}
	}

	if err := l.forge.CloseIssue(ctx, repoPath, url); err != nil {
		return refuse(err.Error())
	}

	var detail string
	if forced {
		detail = "forced: origin ref pruned, branch at a proven tip"
	}
	if err := l.event(ctx, url, store.EventWorktreeRemoved, detail); err != nil {
		return handled{}, fmt.Errorf("record worktree removed: %w", err)
	}
	runs, err := l.store.WithdrawTicket(ctx, url, l.clock.Now(), merged)
	if err != nil {
		return handled{}, fmt.Errorf("withdraw %s: %w", url, err)
	}
	l.pruneRunLogs(runs)
	return succeeded, nil
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
