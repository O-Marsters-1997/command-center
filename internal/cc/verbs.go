package cc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/tp"
)

const (
	reRunVerb          = plan.VerbReRun
	reCheckVerb        = plan.VerbReCheck
	closePRVerb        = plan.VerbClosePR
	removeWorktreeVerb = plan.VerbRemoveWorktree
	cancelVerb         = plan.VerbCancel
	abortVerb          = plan.VerbAbort
)

// supportedVerbs is every verb handleVerb (server.go) accepts.
var supportedVerbs = map[string]bool{
	killVerb:           true,
	retryPushVerb:      true,
	reRunVerb:          true,
	reCheckVerb:        true,
	closePRVerb:        true,
	removeWorktreeVerb: true,
	cancelVerb:         true,
	refreshVerb:        true,
	abortVerb:          true,
}

const (
	eventReRunRefused          = "re_run_refused"
	eventReCheckRequested      = "re_check_requested"
	eventReCheckRefused        = "re_check_refused"
	eventClosePRRequested      = "close_pr_requested"
	eventClosePRFailed         = "close_pr_failed"
	eventRemoveWorktreeRefused = "remove_worktree_refused"
	eventWorktreeRemoved       = "worktree_removed"
	eventLaunchCancelled       = "launch_cancelled"
	eventMergeAborted          = "merge_aborted"
	eventMergeAbortFailed      = "merge_abort_failed"
)

// applyAbortIntents consumes every pending abort request: `git merge --abort` in the worktree,
// the one verb `refresh conflicted` offers. It runs before the refresh step, whose own step 1
// refuses to touch a worktree left mid-merge (docs/designs/command-centre-design.md § 4a).
func (l *Loop) applyAbortIntents(ctx context.Context, obs Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, abortVerb)
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

	now := l.now()
	for _, intent := range intents {
		if task, ok := byTicket[intent.TaskID]; ok {
			if err := l.abortOne(ctx, task, obs, now); err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// abortOne aborts one task's unresolved merge, refusing a worktree a live agent owns (inv. 4),
// and clears the mid-merge the same tick's refresh step reads
// (docs/designs/command-centre-design.md § 4a).
func (l *Loop) abortOne(ctx context.Context, task Task, obs Observation, now time.Time) error {
	fail := func(detail string) error {
		return l.store.AppendEvent(ctx,
			Event{At: now, TaskURL: task.TicketURL, Kind: eventMergeAbortFailed, Detail: detail})
	}

	worktreePath, ok := obs.Worktrees[task.Branch]
	if !ok {
		return fail(fmt.Sprintf("no worktree for %s", task.Branch))
	}
	if obs.Runs[task.TicketURL].Alive {
		return fail(fmt.Sprintf("a run is alive in %s", worktreePath))
	}
	if err := MergeAbort(ctx, worktreePath); err != nil {
		return fail(err.Error())
	}

	delete(obs.MidMerge, task.Branch)
	return l.store.AppendEvent(ctx, Event{At: now, TaskURL: task.TicketURL, Kind: eventMergeAborted})
}

func (l *Loop) applyCancelIntents(ctx context.Context) error {
	intents, err := l.store.PendingVerbIntents(ctx, cancelVerb)
	if err != nil {
		return err
	}

	now := l.now()
	for _, intent := range intents {
		members, err := l.store.CancelLaunchesFor(ctx, intent.TaskID)
		if err != nil {
			return err
		}
		if err := l.store.AppendEvent(ctx, Event{
			At: now, TaskURL: intent.TaskID, Kind: eventLaunchCancelled,
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

// applyReRunIntents consumes every pending re-run request: relaunch in the same worktree,
// incrementally -- a second `runs` row against the same task (docs/prds/prd-command-centre.md §
// Phase 6). Unlike a fresh launch, re-run is not gated by unlock, authorisation or a
// prompt-hash match: it is a human's explicit, one-off decision, not the tick's own
// eligibility check.
func (l *Loop) applyReRunIntents(ctx context.Context, obs Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, reRunVerb)
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
	repoPaths := repoPathsByName(l.ws.Root, l.cfg.Repos)
	retirements := retirementsByName(l.cfg.Seams, planTasksByURL(tasks), prsByBranch(obs), repoPaths)
	authorisedHashes, err := l.store.ActiveLaunchHashes(ctx)
	if err != nil {
		return err
	}
	latest, err := l.store.LatestRunsByTask(ctx)
	if err != nil {
		return err
	}

	now := l.now()
	for _, intent := range intents {
		if task, ok := byTicket[intent.TaskID]; ok {
			err := l.reRunOne(ctx, task, repoPaths[task.Repo], obs, authorisedHashes[task.TicketURL], now, retirements)
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

// reRunOne spawns a new run in a task's existing worktree, baselined off its current tip so
// disposition counts only the commits this new run itself produces, never a previous run's.
func (l *Loop) reRunOne(
	ctx context.Context, task Task, repoPath string, obs Observation, promptHash string, now time.Time,
	retirements map[string]retirement,
) error {
	worktreePath, ok := obs.Worktrees[task.Branch]
	if !ok {
		return l.store.AppendEvent(ctx, Event{
			At: now, TaskURL: task.TicketURL, Kind: eventReRunRefused,
			Detail: fmt.Sprintf("no worktree for %s", task.Branch),
		})
	}

	baselineSHA, err := BranchTip(ctx, repoPath, task.Branch)
	if err != nil {
		return fmt.Errorf("read baseline for re-run of %s: %w", task.TicketURL, err)
	}
	return l.spawnRun(ctx, task, worktreePath, baselineSHA, promptHash, retirements)
}

// applyReCheckIntents consumes every pending re-check request: `gh run rerun <id>`, the compat
// check's own GitHub Actions run (docs/prds/prd-command-centre.md § Phase 5).
func (l *Loop) applyReCheckIntents(ctx context.Context, obs Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, reCheckVerb)
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
	repoPaths := repoPathsByName(l.ws.Root, l.cfg.Repos)
	compatChecks := compatCheckByRepo(l.cfg.Repos)

	now := l.now()
	for _, intent := range intents {
		if task, ok := byTicket[intent.TaskID]; ok {
			err := l.reCheckOne(ctx, task, repoPaths[task.Repo], compatChecks[task.Repo], obs, now)
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

// reCheckOne re-runs the compat check's own GitHub Actions run, named by the run id embedded in
// its DetailsURL (https://github.com/<owner>/<repo>/actions/runs/<run-id>/job/<job-id>).
func (l *Loop) reCheckOne(
	ctx context.Context, task Task, repoPath, compatCheck string, obs Observation, now time.Time,
) error {
	refuse := func(detail string) error {
		return l.store.AppendEvent(ctx,
			Event{At: now, TaskURL: task.TicketURL, Kind: eventReCheckRefused, Detail: detail})
	}
	if compatCheck == "" {
		return refuse("no compat check configured for this repo")
	}

	detailsURL := obs.PRs[task.Branch].Checks[compatCheck].DetailsURL
	runID, err := runIDFromDetailsURL(detailsURL)
	if err != nil {
		return refuse(err.Error())
	}

	if err := gh.Rerun(ctx, repoPath, runID); err != nil {
		return refuse(err.Error())
	}

	// Zeroed exactly as RecordPush zeroes it (pushes.go); issue #56 AC1 requires the row read
	// checking again on the tick after.
	if err := l.store.resetCheckingTicks(ctx, task.TicketURL); err != nil {
		return err
	}
	return l.store.AppendEvent(ctx, Event{At: now, TaskURL: task.TicketURL, Kind: eventReCheckRequested})
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

// applyClosePRIntents consumes every pending close-pr request: `gh pr close`, the sanctioned way
// to unopen a pull request the app opened (docs/prds/prd-command-centre.md § The states). The next
// tick's fallback PR read is what turns this into a derived `pr_closed_unmerged` row.
func (l *Loop) applyClosePRIntents(ctx context.Context) error {
	intents, err := l.store.PendingVerbIntents(ctx, closePRVerb)
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
	repoPaths := repoPathsByName(l.ws.Root, l.cfg.Repos)

	now := l.now()
	for _, intent := range intents {
		if task, ok := byTicket[intent.TaskID]; ok {
			event := Event{At: now, TaskURL: task.TicketURL, Kind: eventClosePRRequested}
			if err := gh.Close(ctx, repoPaths[task.Repo], task.Branch); err != nil {
				event = Event{At: now, TaskURL: task.TicketURL, Kind: eventClosePRFailed, Detail: err.Error()}
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

// applyRemoveWorktreeIntents consumes every pending remove-worktree request: `tp remove
// --force`, called only with MERGED PR state or on a base_gone row the user clears, and never
// on a worktree that is dirty or holds unpushed commits (inv. 3). It prunes that task's run
// logs when it does remove.
func (l *Loop) applyRemoveWorktreeIntents(ctx context.Context, obs Observation) error {
	intents, err := l.store.PendingVerbIntents(ctx, removeWorktreeVerb)
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
	latest, err := l.store.LatestRunsByTask(ctx)
	if err != nil {
		return err
	}
	lastPushed, err := l.store.LastPushedTips(ctx)
	if err != nil {
		return err
	}
	rc := removeWorktreeContext{
		byURL:      planTasksByURL(tasks),
		prs:        prsByBranch(obs),
		stacking:   stackingByRepo(l.cfg.Repos),
		repoPaths:  repoPathsByName(l.ws.Root, l.cfg.Repos),
		lastPushed: lastPushed,
		obs:        obs,
	}
	byTicket := tasksByTicket(tasks)

	now := l.now()
	for _, intent := range intents {
		if task, ok := byTicket[intent.TaskID]; ok {
			_, hasRun := latest[task.TicketURL]
			if err := l.removeWorktreeOne(ctx, task, rc, hasRun, now); err != nil {
				return err
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// removeWorktreeContext is the per-tick facts removeWorktreeOne needs, gathered once by
// applyRemoveWorktreeIntents rather than re-queried per task -- pushContext's own shape (push.go).
type removeWorktreeContext struct {
	byURL     map[string]plan.Task
	prs       map[string]plan.PRState
	stacking  map[string]bool
	repoPaths map[string]string
	// lastPushed is what HasUnpushedCommits falls back to once GitHub's delete-branch-on-merge
	// and our own fetch --prune have removed the remote-tracking ref a merged branch was pushed to.
	lastPushed map[string]string
	obs        Observation
}

// removeWorktreeOne applies inv. 3's gate, in order: is this row even eligible (merged, or
// base_gone with hasRun) — the cheap check, no git calls — then is the worktree dirty, then
// does it hold unpushed commits. Any refusal is recorded as an event and nothing is removed;
// only a clean pass through every gate reaches tp.Remove.
func (l *Loop) removeWorktreeOne(
	ctx context.Context, task Task, rc removeWorktreeContext, hasRun bool, now time.Time,
) error {
	refuse := func(detail string) error {
		return l.store.AppendEvent(ctx,
			Event{At: now, TaskURL: task.TicketURL, Kind: eventRemoveWorktreeRefused, Detail: detail})
	}

	worktreePath, ok := rc.obs.Worktrees[task.Branch]
	if !ok {
		return refuse(fmt.Sprintf("no worktree for %s", task.Branch))
	}

	merged := rc.obs.PRs[task.Branch].State == gh.Merged
	unlock := plan.Unlocked(rc.byURL[task.TicketURL], rc.byURL, rc.prs, rc.stacking[task.Repo])
	baseGone := hasRun && unlock.BlockerClosed
	if !merged && !baseGone {
		return refuse("neither merged nor base gone")
	}

	repoPath := rc.repoPaths[task.Repo]
	dirty, err := IsDirty(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("check dirty for %s: %w", task.TicketURL, err)
	}
	if dirty {
		return refuse("worktree is dirty")
	}

	unpushed, err := HasUnpushedCommits(ctx, repoPath, task.Branch, rc.lastPushed[task.TicketURL])
	if err != nil {
		return fmt.Errorf("check unpushed commits for %s: %w", task.TicketURL, err)
	}
	if unpushed {
		return refuse("worktree holds unpushed commits")
	}

	if err := tp.Remove(ctx, repoPath, task.Branch); err != nil {
		return refuse(err.Error())
	}

	if err := l.pruneRunLogs(ctx, task.TicketURL); err != nil {
		return err
	}
	return l.store.AppendEvent(ctx, Event{At: now, TaskURL: task.TicketURL, Kind: eventWorktreeRemoved})
}

// pruneRunLogs deletes every runs/<id>.jsonl, runs/<id>.prompt and runs/<id>.diff a task's runs
// ever produced. Best-effort: a cut-failed run never wrote any of them, and most runs never wrote
// a diff at all, so a missing file is not an error.
func (l *Loop) pruneRunLogs(ctx context.Context, taskID string) error {
	ids, err := l.store.RunIDsForTask(ctx, taskID)
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
