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

func (l *Loop) applyAbortIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return l.eachIntentEntry(ctx, snap, plan.VerbAbort, func(_ plan.Intent, e plan.Entry) error {
		return l.abortOne(ctx, e.Ticket, obs)
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
	return l.eachIntentEntry(ctx, snap, plan.VerbFollowUp, func(intent plan.Intent, e plan.Entry) error {
		ticket, ok := byURL[e.Ticket.URL]
		if !ok {
			return nil
		}
		picked := store.DecodeFollowUpPayload(intent.Payload)
		return l.followUpOne(ctx, snap, t, ticket, picked, plan.CauseNone)
	})
}

func (l *Loop) followUpOne(
	ctx context.Context, snap plan.Snapshot, t *tick, ticket store.Ticket,
	picked store.FollowUpPayload, cause plan.FollowUpCause,
) error {
	promptText := picked.Message
	url := plan.TicketURL(ticket.URL)
	if t.obs.Runs[url].Alive {
		return errIntentPending
	}
	worktreePath, refusal := idleWorktreeFor(ticket.Plan(), t.obs)
	if refusal != "" {
		return l.event(ctx, ticket.URL, store.EventFollowUpRefused, refusal)
	}

	baselineSHA, err := l.baseline(ctx, ticket)
	if err != nil {
		return err
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
		return fmt.Errorf("find resumable session: %w", err)
	}
	if sessionID != "" {
		outcome := plan.OutcomeFailed
		if launched.Run.IsSome() {
			outcome = launched.Run.Unwrap().Outcome.UnwrapOr(plan.OutcomeFailed)
		}
		spec.prompt = plan.ComposeMessage(promptText, outcome)
		spec.resumeSessionID = sessionID
	}
	if launched.State == plan.CIFailed || launched.State == plan.Parked {
		logs, err := l.failedLogs(ctx, ticket, launched, t.obs)
		if err != nil {
			return err
		}
		spec.prompt += logs
	}
	if err := l.spawnRun(ctx, t, spec); err != nil {
		return fmt.Errorf("spawn follow-up: %w", err)
	}
	if slices.Contains(t.spawned, url) {
		t.obs.MarkAlive(url)
	}
	return nil
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
	return l.eachPendingIntent(ctx, plan.VerbCancel, func(intent plan.Intent) error {
		ticketID := string(intent.Ticket)
		switch err := l.store.DisarmTicket(ctx, ticketID); {
		case errors.Is(err, store.ErrArmedDependent):
			return l.event(ctx, ticketID, store.EventLaunchCancelled, "disarm refused: "+err.Error())
		case err != nil:
			return fmt.Errorf("disarm ticket: %w", err)
		}
		return l.event(ctx, ticketID, store.EventLaunchCancelled, "ticket disarmed")
	})
}

func (l *Loop) applyReRunIntents(
	ctx context.Context, snap plan.Snapshot, t *tick, byURL map[plan.TicketURL]store.Ticket,
) error {
	return l.eachIntentEntry(ctx, snap, plan.VerbReRun, func(_ plan.Intent, entry plan.Entry) error {
		ticket, ok := byURL[entry.Ticket.URL]
		if !ok {
			return nil
		}
		baseBranch := entry.Unlock.BaseBranch
		if baseBranch == "" {
			baseBranch = plan.DefaultBaseBranch
		}
		return l.reRunOne(ctx, t, ticket, entry, baseBranch)
	})
}

func (l *Loop) reRunOne(
	ctx context.Context, t *tick, ticket store.Ticket, entry plan.Entry, baseBranch string,
) error {
	launched, promptHash := entry.Ticket, entry.PromptHash
	begin := l.startOf(entry, t.obs)
	worktreePath, ok := t.obs.Worktrees[ticket.Ref()]
	if !ok {
		if err := git.DeleteBranchIfExists(ctx, l.checkout(ticket.Repo), ticket.Branch); err != nil {
			return fmt.Errorf("clear stale branch before re-cutting %s: %w", ticket.Branch, err)
		}
		if err := l.cutAndSpawn(ctx, t, ticket, launched, baseBranch, promptHash, begin); err != nil {
			return fmt.Errorf("re-cut %s: %w", ticket.Branch, err)
		}
		return nil
	}

	baselineSHA, err := l.baseline(ctx, ticket)
	if err != nil {
		return err
	}
	if err := l.spawnRun(ctx, t, begin.spec(ticket, launched, worktreePath, baselineSHA, promptHash, nil)); err != nil {
		return fmt.Errorf("spawn re-run in %s: %w", worktreePath, err)
	}
	return nil
}

func (l *Loop) applyRemoveWorktreeIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	handled := map[plan.TicketURL]bool{}
	return l.eachIntentEntry(ctx, snap, plan.VerbRemoveWorktree, func(intent plan.Intent, e plan.Entry) error {
		if handled[intent.Ticket] {
			return nil
		}
		handled[intent.Ticket] = true
		return l.removeWorktreeOne(ctx, e, snap.Removal(e.Ticket.URL), obs)
	})
}

func (l *Loop) queueAutoRemovals(ctx context.Context, snap plan.Snapshot) error {
	for _, url := range snap.AutoRemovals() {
		request := store.VerbRequest{Ticket: string(url), Verb: plan.VerbRemoveWorktree, At: l.clock.Now()}
		if _, err := l.store.QueueVerbIntent(ctx, request); err != nil {
			return fmt.Errorf("queue removal of %s: %w", url, err)
		}
	}
	return nil
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

	forced := false
	if _, present := obs.Worktrees[ticket.Ref()]; present {
		var refusal string
		var err error
		if forced, refusal, err = l.removeWorktree(ctx, ticket, removal); err != nil {
			return err
		}
		if refusal != "" {
			return refuse(refusal)
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
		return fmt.Errorf("record worktree removed: %w", err)
	}
	runs, err := l.store.WithdrawTicket(ctx, url, l.clock.Now(), merged)
	if err != nil {
		return fmt.Errorf("withdraw %s: %w", url, err)
	}
	l.pruneRunLogs(runs)
	return nil
}

func (l *Loop) removeWorktree(
	ctx context.Context, ticket plan.Ticket, removal plan.Removal,
) (forced bool, refusal string, err error) {
	url := string(ticket.URL)
	repoPath := l.checkout(ticket.Repo)
	provenTips := make([]string, len(removal.ProvenTips))
	for i, tip := range removal.ProvenTips {
		provenTips[i] = string(tip)
	}
	state, err := git.RemovalStateFor(ctx, repoPath, ticket.Branch, provenTips...)
	if err != nil {
		return false, "", fmt.Errorf("check unpushed commits for %s: %w", url, err)
	}
	switch state {
	case git.RemovableByMerged:
	case git.RemovableByForce:
		forced = true
	case git.NotRemovable:
		return false, "worktree holds unpushed commits", nil
	case git.RemovalUnknown:
		return false, "", fmt.Errorf("check unpushed commits for %s: removal state is unknown", url)
	}

	err = l.worktrees.Remove(ctx, repoPath, ticket.Branch)
	switch {
	case errors.Is(err, treepad.ErrPostHook):
		l.logger.Warn("post hook failed after removing worktree", "ticket", url, "err", err)
	case errors.Is(err, treepad.ErrDirty):
		return false, "worktree is dirty", nil
	case err != nil:
		return false, err.Error(), nil
	}
	return forced, "", nil
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
