package loop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	reRunVerb          = plan.VerbReRun
	reCheckVerb        = plan.VerbReCheck
	closePRVerb        = plan.VerbClosePR
	removeWorktreeVerb = plan.VerbRemoveWorktree
	cancelVerb         = plan.VerbCancel
	abortVerb          = plan.VerbAbort
	resolveVerb        = plan.VerbResolve
	followUpVerb       = plan.VerbFollowUp
)

const (
	eventReCheckRequested         = "re_check_requested"
	eventReCheckRefused           = "re_check_refused"
	eventClosePRRequested         = "close_pr_requested"
	eventClosePRFailed            = "close_pr_failed"
	eventWorktreeRemoved          = "worktree_removed"
	eventLaunchCancelled          = "launch_cancelled"
	eventMergeAborted             = "merge_aborted"
	eventMergeAbortFailed         = "merge_abort_failed"
	eventResolveRefused           = "resolve_refused"
	eventFollowUpRefused          = "follow_up_refused"
	eventFollowUpCILogUnavailable = "follow_up_ci_log_unavailable"
)

func (l *Loop) applyAbortIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, abortVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	now := l.clock.Now()
	for _, intent := range intents {
		if e, ok := snap.Entry(intent.TicketID); ok {
			if err := l.abortOne(ctx, e.Ticket, obs, now); err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) abortOne(ctx context.Context, ticket plan.Ticket, obs plan.Observation, now time.Time) error {
	fail := func(detail string) error {
		return l.store.AppendEvent(ctx,
			store.Event{At: now, TicketURL: ticket.URL, Kind: eventMergeAbortFailed, Detail: detail})
	}

	worktreePath, ok := obs.Worktrees[branchKey(ticket.Repo, ticket.Branch)]
	if !ok {
		return fail(fmt.Sprintf("no worktree for %s", ticket.Branch))
	}
	if obs.Runs[ticket.URL].Alive {
		return fail(fmt.Sprintf("a run is alive in %s", worktreePath))
	}
	if err := git.MergeAbort(ctx, worktreePath); err != nil {
		return fail(err.Error())
	}

	delete(obs.MidMerge, branchKey(ticket.Repo, ticket.Branch))
	return l.store.AppendEvent(ctx, store.Event{At: now, TicketURL: ticket.URL, Kind: eventMergeAborted})
}

func (l *Loop) applyResolveIntents(ctx context.Context, obs plan.Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, resolveVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}
	byTicket := ticketsByURL(tickets)
	repoPaths := repoPathsByName(l.cfg.Repos)

	now := l.clock.Now()
	for _, intent := range intents {
		if ticket, ok := byTicket[intent.TicketID]; ok {
			if err := l.resolveOne(ctx, ticket, repoPaths[ticket.Repo], obs, now); err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) resolveOne(
	ctx context.Context, ticket store.Ticket, repoPath string, obs plan.Observation, now time.Time,
) error {
	worktreePath, refusal := idleWorktreeFor(ticket, obs)
	if refusal != "" {
		return l.store.AppendEvent(ctx,
			store.Event{At: now, TicketURL: ticket.URL, Kind: eventResolveRefused, Detail: refusal})
	}

	baselineSHA, err := git.BranchTip(ctx, repoPath, ticket.Branch)
	if err != nil {
		return fmt.Errorf("read baseline for resolve of %s: %w", ticket.URL, err)
	}
	return l.spawnRun(ctx, ticket, worktreePath, baselineSHA, "", "", runKindResolve, "", "")
}

func idleWorktreeFor(ticket store.Ticket, obs plan.Observation) (worktreePath, refusal string) {
	worktreePath, ok := obs.Worktrees[branchKey(ticket.Repo, ticket.Branch)]
	if !ok {
		return "", fmt.Sprintf("no worktree for %s", ticket.Branch)
	}
	if obs.Runs[ticket.URL].Alive {
		return "", fmt.Sprintf("a run is alive in %s", worktreePath)
	}
	return worktreePath, ""
}

func (l *Loop) applyFollowUpIntents(ctx context.Context, obs plan.Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, followUpVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}
	byTicket := ticketsByURL(tickets)
	repoPaths := repoPathsByName(l.cfg.Repos)
	pushFacts, err := l.store.PushFacts(ctx)
	if err != nil {
		return err
	}
	vd, err := l.store.VerdictFacts(ctx)
	if err != nil {
		return err
	}

	now := l.clock.Now()
	for _, intent := range intents {
		if ticket, ok := byTicket[intent.TicketID]; ok {
			err := l.followUpOne(ctx, ticket, repoPaths[ticket.Repo], intent.Payload, obs, vd, pushFacts, now)
			if err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) followUpOne(
	ctx context.Context, ticket store.Ticket, repoPath, promptText string, obs plan.Observation, vd plan.VerdictFacts,
	pushFacts map[string]plan.PushFact, now time.Time,
) error {
	worktreePath, refusal := idleWorktreeFor(ticket, obs)
	if refusal != "" {
		return l.store.AppendEvent(ctx,
			store.Event{At: now, TicketURL: ticket.URL, Kind: eventFollowUpRefused, Detail: refusal})
	}

	baselineSHA, err := git.BranchTip(ctx, repoPath, ticket.Branch)
	if err != nil {
		return fmt.Errorf("read baseline for follow-up of %s: %w", ticket.URL, err)
	}

	ciSection, unavailableDetail := l.fetchCIFailedLog(ctx, ticket, repoPath, obs, vd, pushFacts)
	if unavailableDetail != "" {
		if err := l.store.AppendEvent(ctx, store.Event{
			At: now, TicketURL: ticket.URL, Kind: eventFollowUpCILogUnavailable, Detail: unavailableDetail,
		}); err != nil {
			return err
		}
	}
	return l.spawnRun(ctx, ticket, worktreePath, baselineSHA, "", "", runKindFollowUp, promptText, ciSection)
}

const ciLogUnavailableSection = "## Failed CI log\n\n" +
	"The failed job's log could not be retrieved. Treat the CI failure as unverified: you have not seen the log."

func (l *Loop) fetchCIFailedLog(
	ctx context.Context, ticket store.Ticket, repoPath string, obs plan.Observation, vd plan.VerdictFacts,
	pushFacts map[string]plan.PushFact,
) (section, unavailableDetail string) {
	pf := pushFacts[ticket.URL]
	if pf.Refused || pf.Failed || obs.PRs[branchKey(ticket.Repo, ticket.Branch)].State != plan.Open {
		return "", ""
	}

	fact := &plan.RunFact{PROpen: true}
	l.cfg.PlanRules().ApplyVerdict(fact, ticket.Plan(), obs, vd)
	if !fact.VerdictCIFailed {
		return "", ""
	}

	checks := obs.PRs[branchKey(ticket.Repo, ticket.Branch)].Checks
	var detailsURL string
	for _, name := range fact.RedLeaves {
		if url := checks[name].DetailsURL; url != "" {
			detailsURL = url
			break
		}
	}
	if detailsURL == "" {
		return ciLogUnavailableSection, "red check names no Actions run id"
	}

	runID, err := runIDFromDetailsURL(detailsURL)
	if err != nil {
		return ciLogUnavailableSection, err.Error()
	}

	log, err := l.forge.RunViewLogFailed(ctx, repoPath, runID)
	if err != nil {
		return ciLogUnavailableSection, err.Error()
	}
	return "## Failed CI log\n\nLast 200 lines of the failed job's log:\n\n```\n" + lastLines(log, 200) + "\n```", ""
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (l *Loop) applyCancelIntents(ctx context.Context) error {
	intents, err := l.store.PendingVerbIntents(ctx, cancelVerb)
	if err != nil {
		return err
	}

	now := l.clock.Now()
	for _, intent := range intents {
		members, err := l.store.CancelLaunchesFor(ctx, intent.TicketID)
		if err != nil {
			return err
		}
		if err := l.store.AppendEvent(ctx, store.Event{
			At: now, TicketURL: intent.TicketID, Kind: eventLaunchCancelled,
			Detail: fmt.Sprintf("launch cancelled, %d member(s)", members),
		}); err != nil {
			return err
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) applyReRunIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, reRunVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}
	byTicket := ticketsByURL(tickets)
	repoPaths := repoPathsByName(l.cfg.Repos)
	latest, err := l.store.LatestRunsByTicket(ctx)
	if err != nil {
		return err
	}

	now := l.clock.Now()
	for _, intent := range intents {
		if ticket, ok := byTicket[intent.TicketID]; ok {
			var oldPromptPath string
			if run, ok := latest[ticket.URL]; ok {
				oldPromptPath = filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.prompt", run.ID))
			}
			entry, _ := snap.Entry(ticket.URL)
			baseBranch := entry.Unlock.BaseBranch
			if baseBranch == "" {
				baseBranch = defaultBaseBranch
			}
			err := l.reRunOne(
				ctx, ticket, repoPaths[ticket.Repo], baseBranch, obs, entry.PromptHash, now, oldPromptPath,
			)
			if err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) reRunOne(
	ctx context.Context, ticket store.Ticket, repoPath, baseBranch string, obs plan.Observation, promptHash string,
	now time.Time, oldPromptPath string,
) error {
	worktreePath, ok := obs.Worktrees[branchKey(ticket.Repo, ticket.Branch)]
	if !ok {
		if err := git.DeleteBranchIfExists(ctx, repoPath, ticket.Branch); err != nil {
			return fmt.Errorf("clear stale branch before re-cutting %s: %w", ticket.Branch, err)
		}
		return l.cutAndSpawn(ctx, launchSpec{
			ticket: ticket, baseBranch: baseBranch, promptHash: promptHash, repoPath: repoPath,
		})
	}

	baselineSHA, err := git.BranchTip(ctx, repoPath, ticket.Branch)
	if err != nil {
		return fmt.Errorf("read baseline for re-run of %s: %w", ticket.URL, err)
	}
	return l.spawnRun(ctx, ticket, worktreePath, baselineSHA, promptHash, oldPromptPath, runKindAgent, "", "")
}

func (l *Loop) applyReCheckIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, reCheckVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	repoPaths := repoPathsByName(l.cfg.Repos)
	compatChecks := l.cfg.PlanRules().CompatCheck

	now := l.clock.Now()
	for _, intent := range intents {
		if e, ok := snap.Entry(intent.TicketID); ok {
			ticket := e.Ticket
			err := l.reCheckOne(ctx, ticket, repoPaths[ticket.Repo], compatChecks[ticket.Repo], obs, now)
			if err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) reCheckOne(
	ctx context.Context, ticket plan.Ticket, repoPath, compatCheck string, obs plan.Observation, now time.Time,
) error {
	refuse := func(detail string) error {
		return l.store.AppendEvent(ctx,
			store.Event{At: now, TicketURL: ticket.URL, Kind: eventReCheckRefused, Detail: detail})
	}
	if compatCheck == "" {
		return refuse("no compat check configured for this repo")
	}

	detailsURL := obs.PRs[branchKey(ticket.Repo, ticket.Branch)].Checks[compatCheck].DetailsURL
	runID, err := runIDFromDetailsURL(detailsURL)
	if err != nil {
		return refuse(err.Error())
	}

	if err := l.forge.Rerun(ctx, repoPath, runID); err != nil {
		return refuse(err.Error())
	}

	if err := l.store.ResetCheckingTicks(ctx, ticket.URL); err != nil {
		return err
	}
	return l.store.AppendEvent(ctx, store.Event{At: now, TicketURL: ticket.URL, Kind: eventReCheckRequested})
}

// runIDFromDetailsURL parses the run id out of a check's DetailsURL
// (https://github.com/<owner>/<repo>/actions/runs/<run-id>/job/<job-id>).
func runIDFromDetailsURL(detailsURL string) (string, error) {
	const marker = "/actions/runs/"
	i := strings.Index(detailsURL, marker)
	if i < 0 {
		return "", fmt.Errorf("compat check details url %q has no /actions/runs/<id> segment", detailsURL)
	}
	id, _, _ := strings.Cut(detailsURL[i+len(marker):], "/")
	if id == "" {
		return "", fmt.Errorf("compat check details url %q has no /actions/runs/<id> segment", detailsURL)
	}
	return id, nil
}

func (l *Loop) applyClosePRIntents(ctx context.Context, snap plan.Snapshot) error {
	intents, err := l.store.PendingVerbIntents(ctx, closePRVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	repoPaths := repoPathsByName(l.cfg.Repos)

	now := l.clock.Now()
	for _, intent := range intents {
		if e, ok := snap.Entry(intent.TicketID); ok {
			ticket := e.Ticket
			event := store.Event{At: now, TicketURL: ticket.URL, Kind: eventClosePRRequested}
			if err := l.forge.Close(ctx, repoPaths[ticket.Repo], ticket.Branch); err != nil {
				event = store.Event{At: now, TicketURL: ticket.URL, Kind: eventClosePRFailed, Detail: err.Error()}
			}
			if err := l.store.AppendEvent(ctx, event); err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) applyRemoveWorktreeIntents(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, removeWorktreeVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	lastPushed, err := l.store.LastPushedTips(ctx)
	if err != nil {
		return err
	}

	now := l.clock.Now()
	for _, intent := range intents {
		if e, ok := snap.Entry(intent.TicketID); ok {
			if err := l.removeWorktreeOne(ctx, e, obs, lastPushed[e.Ticket.URL], now); err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) removeWorktreeOne(
	ctx context.Context, e plan.Entry, obs plan.Observation, lastPushed string, now time.Time,
) error {
	ticket := e.Ticket
	refuse := func(detail string) error {
		return l.store.AppendEvent(ctx,
			store.Event{At: now, TicketURL: ticket.URL, Kind: store.EventRemoveWorktreeRefused, Detail: detail})
	}

	merged := obs.PRs[branchKey(ticket.Repo, ticket.Branch)].State == plan.Merged
	baseGone := e.Run != nil && e.Unlock.BlockerClosed
	if !merged && !baseGone {
		return refuse("neither merged nor base gone")
	}

	repoPath := repoPathsByName(l.cfg.Repos)[ticket.Repo]
	worktreePath, worktreePresent := obs.Worktrees[branchKey(ticket.Repo, ticket.Branch)]

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
	}

	if worktreePresent {
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
	if err := l.store.AppendEvent(ctx,
		store.Event{At: now, TicketURL: ticket.URL, Kind: eventWorktreeRemoved, Detail: detail}); err != nil {
		return err
	}
	return l.store.WithdrawTicket(ctx, ticket.URL, now, merged)
}

func (l *Loop) pruneRunLogs(ctx context.Context, ticketID string) error {
	ids, err := l.store.RunIDsForTicket(ctx, ticketID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		_ = os.Remove(filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.jsonl", id)))
		_ = os.Remove(filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.prompt", id)))
		_ = os.Remove(filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.diff", id)))
	}
	return nil
}
