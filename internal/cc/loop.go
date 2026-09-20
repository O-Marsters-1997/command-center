package cc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/tp"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// tickPeriod is the sleep *after* work: ticks never overlap, and the loop never branches on
// why it woke.
const tickPeriod = 15 * time.Second

const killVerb = plan.VerbKill

const (
	runKindAgent    = "agent"
	runKindResolve  = "resolve"
	runKindFollowUp = "follow_up"
)

// Event kinds a launch (fresh or re-run), a disposition or a verdict transition append —
// alongside push.go's push_refused/push_failed and verbs.go's remove/close-pr kinds, what lets
// `events` reconstruct the whole run (docs/prds/prd-command-centre.md § Phase 6).
const (
	eventRunLaunched = "run_launched"
	eventRunDisposed = "run_disposed"
	eventReRunNoDiff = "re_run_no_diff"
)

// TickError is the last failed tick, rendered on the page with its age.
type TickError struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

// Loop is the reconcile loop: observe, decide, act. It is the only writer of the database
// (inv. 9).
type Loop struct {
	store      *Store
	observe    ObserveFunc
	now        func() time.Time
	runner     Runner
	cfg        Config
	ws         Workspace
	trackerFor TrackerSource
	nudgeCh    chan struct{}
}

// NewLoop assembles the loop over an observe phase, a clock and the configuration a tick's cut
// and spawn steps need (repos, agent_command, max_agents, the state dir's runs and settings
// paths). runner is the seam a test substitutes for real process spawning, liveness and cancel.
func NewLoop(store *Store, observe ObserveFunc, now func() time.Time, cfg Config, ws Workspace, runner Runner) *Loop {
	return &Loop{
		store: store, observe: observe, now: now, runner: runner, cfg: cfg, ws: ws, trackerFor: tracker.New,
		nudgeCh: make(chan struct{}, 1),
	}
}

// Nudge wakes Run for one tick right now rather than at the end of tickPeriod. A nudge that
// finds the buffer full is dropped, not queued: the tick already in flight will pick up
// whatever intent prompted it anyway (docs/adr/0014-opening-the-launch-modal-imports-the-feature.md).
func (l *Loop) Nudge() {
	select {
	case l.nudgeCh <- struct{}{}:
	default:
	}
}

// SetTrackerSource replaces the loop's tracker.New, so a test can drive applyImportIntents with a
// fake source rather than shelling out to gh.
func (l *Loop) SetTrackerSource(resolve TrackerSource) { l.trackerFor = resolve }

// RunOnce runs one tick. A failed observe records the error and leaves the last good
// observation in place rather than applying any transition, so the page's observe age
// keeps growing instead of resetting (inv. 10).
func (l *Loop) RunOnce(ctx context.Context) error {
	// Runs before observe, not after like every other applyXIntents: a ticket imported this
	// tick then has its branch and PR read in the same observe pass, rather than sitting one
	// tick behind.
	if err := l.applyImportIntents(ctx); err != nil {
		return err
	}
	// Same reasoning as the import step above: an edited branch is read by this tick's own
	// worktree lookup and unlock decision, not next tick's.
	if err := l.applyEditTicketIntents(ctx); err != nil {
		return err
	}

	obs, err := l.observe(ctx)
	if err != nil {
		at := l.now()
		tickErr := fmt.Errorf("observe: %w", err)
		if recordErr := l.store.RecordTickError(ctx, TickError{At: at, Message: tickErr.Error()}); recordErr != nil {
			return recordErr
		}
		return tickErr
	}

	obs.ObservedAt = l.now()
	if obs.Runs == nil {
		obs.Runs = map[string]RunObservation{}
	}
	if err := l.tickCheckingWaits(ctx); err != nil {
		return err
	}
	if err := l.store.SaveObservation(ctx, obs); err != nil {
		return err
	}
	if err := l.store.ApplyLaunchIntents(ctx, l.now()); err != nil {
		return err
	}
	if err := l.applyCancelIntents(ctx); err != nil {
		return err
	}
	if err := l.applyKillIntents(ctx); err != nil {
		return err
	}
	if err := l.reconcileRuns(ctx, obs); err != nil {
		return err
	}
	// reconcileRuns mutated obs.Runs (the map is shared, obs itself is a copy) with this
	// tick's own liveness reads — saved again so the page can render them after a restart
	// without re-probing between requests.
	if err := l.store.SaveObservation(ctx, obs); err != nil {
		return err
	}
	if err := l.retargetMerged(ctx, obs); err != nil {
		return err
	}
	if err := l.applyAbortIntents(ctx, obs); err != nil {
		return err
	}
	if err := l.resolveGeneratedConflicts(ctx, obs); err != nil {
		return err
	}
	if err := l.applyResolveIntents(ctx, obs); err != nil {
		return err
	}
	if err := l.applyRefreshIntents(ctx, obs); err != nil {
		return err
	}
	if err := l.applyRetryPushIntents(ctx, obs); err != nil {
		return err
	}
	if err := l.applyCommitResolutionIntents(ctx, obs); err != nil {
		return err
	}
	if err := l.pushPushable(ctx, obs); err != nil {
		return err
	}
	if err := l.recordVerdictTransitions(ctx, obs); err != nil {
		return err
	}
	if err := l.recordMergedEvents(ctx, obs); err != nil {
		return err
	}
	if err := l.applyDraftGate(ctx, obs); err != nil {
		return err
	}
	if err := l.applyReRunIntents(ctx, obs); err != nil {
		return err
	}
	if err := l.applyFollowUpIntents(ctx, obs); err != nil {
		return err
	}
	if err := l.applyReCheckIntents(ctx, obs); err != nil {
		return err
	}
	if err := l.applyClosePRIntents(ctx); err != nil {
		return err
	}
	if err := l.applyRemoveWorktreeIntents(ctx, obs); err != nil {
		return err
	}
	return l.launchEligible(ctx, obs)
}

// Run ticks until the context is cancelled, sleeping after each tick's work. A tick error is
// already recorded for the page, so the loop logs it and carries on.
func (l *Loop) Run(ctx context.Context) error {
	for {
		if err := l.RunOnce(ctx); err != nil {
			log.Printf("tick: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(tickPeriod):
		case <-l.nudgeCh:
		}
	}
}

// applyImportIntents performs the actual upsert for every pending import request, keeping the
// loop the tickets table's only writer (inv. 9) even for a row that arrived from a queued import
// intent rather than a hand-authored config.
func (l *Loop) applyImportIntents(ctx context.Context) error {
	intents, err := l.store.PendingVerbIntents(ctx, importVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	now := l.now()
	for _, intent := range intents {
		if err := l.importFeature(ctx, intent.TicketID); err != nil {
			return err
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) importFeature(ctx context.Context, feature string) error {
	var matched []ImportedTicket
	for _, repo := range l.cfg.Repos {
		src, ok, err := trackerSourceFor(repo, l.trackerFor)
		if err != nil {
			return fmt.Errorf("import %s: %w", feature, err)
		}
		if !ok {
			continue
		}

		tickets, err := src.Tickets(ctx, feature)
		if err != nil {
			return fmt.Errorf("import %s from %s: %w", feature, repo.Name, err)
		}
		for _, t := range tickets {
			matched = append(matched, ImportedTicket{Ticket: t, Repo: repo.Name, Source: repo.Tracker})
		}
	}

	err := l.store.ImportTickets(ctx, feature, matched, l.now())
	var conflict *FeatureConflictError
	if errors.As(err, &conflict) {
		return l.store.RecordImportRefusal(ctx, feature, conflict, l.now())
	}
	var closure *FeatureClosureError
	if errors.As(err, &closure) {
		return l.store.RecordImportRefusal(ctx, feature, closure, l.now())
	}
	return err
}

// applyEditTicketIntents performs the actual write for every pending POST /ticket request,
// keeping the loop the tickets table's only writer (inv. 9) even for a branch or blocked_by
// edit made from the page.
func (l *Loop) applyEditTicketIntents(ctx context.Context) error {
	intents, err := l.store.PendingEditTicketIntents(ctx)
	if err != nil {
		return err
	}

	now := l.now()
	for _, intent := range intents {
		err := l.store.EditTicket(ctx, intent.TicketID, intent.Branch, intent.BlockedBy)
		var closure *FeatureClosureError
		if errors.As(err, &closure) {
			if err := l.store.RecordImportRefusal(ctx, closure.Feature, closure, now); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// applyKillIntents consumes every pending kill request synchronously: this is the loop's own
// read-then-signal-then-write sequence, never a handler's (a handler only ever queues the
// intent, inv. 9 — see server.go's handleVerb).
func (l *Loop) applyKillIntents(ctx context.Context) error {
	intents, err := l.store.PendingVerbIntents(ctx, killVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	latest, err := l.store.LatestRunsByTicket(ctx)
	if err != nil {
		return err
	}

	now := l.now()
	for _, intent := range intents {
		if run, ok := latest[intent.TicketID]; ok && run.Pgid != nil && !run.HasOutcome {
			if err := l.runner.Cancel(*run.Pgid); err != nil {
				return fmt.Errorf("cancel %s (pgid %d): %w", intent.TicketID, *run.Pgid, err)
			}
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// reconcileRuns is liveness plus disposition, over every run this or a prior instance spawned
// and never disposed of. It runs identically on tick 1 after a restart and on tick 4000 of
// uptime: a re-attached process is found alive the same way a fresh one is (§ Crash recovery).
func (l *Loop) reconcileRuns(ctx context.Context, obs Observation) error {
	pending, err := l.store.PendingRunsAwaitingDisposition(ctx)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}
	byTicket := ticketsByURL(tickets)

	now := l.now()
	for _, run := range pending {
		alive, err := l.runner.Liveness(run.Pgid, run.ProcStartedAt, now)
		if err != nil {
			return fmt.Errorf("liveness for run %d: %w", run.ID, err)
		}
		obs.Runs[run.TicketID] = RunObservation{Alive: alive}
		if alive {
			continue
		}
		if err := l.disposeRun(ctx, run, byTicket[run.TicketID], obs, now); err != nil {
			return err
		}
	}
	return nil
}

// disposeRun computes and records one dead run's outcome (docs/prds/prd-command-centre.md § A run):
// commits after its own baseline decide push vs failed, never a missing event (inv. 7).
func (l *Loop) disposeRun(ctx context.Context, run PendingRun, ticket Ticket, obs Observation, now time.Time) error {
	commits := 0
	if run.BaselineSHA != "" {
		var err error
		commits, err = l.commitsSinceBaseline(ctx, ticket, obs, run.BaselineSHA)
		if err != nil {
			return fmt.Errorf("commits since baseline for run %d: %w", run.ID, err)
		}
	}
	outcome := plan.Disposition(commits)

	var exitCode *int
	if code, ok := l.runner.Reap(run.Pgid); ok {
		exitCode = &code
	}
	if err := l.store.RecordDisposition(ctx, run.ID, outcome, exitCode, now); err != nil {
		return fmt.Errorf("record disposition for run %d: %w", run.ID, err)
	}
	return l.store.AppendEvent(ctx, Event{
		At: now, TicketURL: ticket.URL, Kind: eventRunDisposed, Detail: outcome.String(),
	})
}

// commitsSinceBaseline counts commits after baseline from the ticket's own worktree while it
// still exists, and from origin/<branch> in the repo's own checkout once it does not (issue
// #189): a remove-worktree racing a tick's disposal must not read as zero commits when
// obs.BranchTips already carries that same tip from this tick's own fetch.
func (l *Loop) commitsSinceBaseline(
	ctx context.Context, ticket Ticket, obs Observation, baselineSHA string,
) (int, error) {
	if worktreePath := obs.Worktrees[branchKey(ticket.Repo, ticket.Branch)]; worktreePath != "" {
		return CommitsSince(ctx, worktreePath, baselineSHA, "HEAD")
	}
	tip, ok := obs.BranchTips[branchKey(ticket.Repo, ticket.Branch)]
	if !ok {
		return 0, nil
	}
	repoPath := repoPathsByName(l.cfg.Repos)[ticket.Repo]
	return CommitsSince(ctx, repoPath, baselineSHA, tip)
}

// launchEligible is job 3 of the tick: plan.LaunchPlan picks the tickets to cut and spawn this
// tick, under max_agents applied globally over every repo's unlock results.
func (l *Loop) launchEligible(ctx context.Context, obs Observation) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}
	authorisedHashes, err := l.store.ActiveLaunchHashes(ctx)
	if err != nil {
		return err
	}
	latest, err := l.store.LatestRunsByTicket(ctx)
	if err != nil {
		return err
	}

	stacking := stackingByRepo(l.cfg.Repos)
	byURL := planTicketsByURL(tickets)
	prs := prsByBranch(tickets, obs)
	repoPaths := repoPathsByName(l.cfg.Repos)

	candidates := make([]plan.LaunchCandidate, 0, len(tickets))
	unlocks := make(map[string]plan.Unlock, len(tickets))
	for _, t := range tickets {
		pt := planTicket(t)
		unlock := plan.Unlocked(pt, byURL, prs, stacking[t.Repo])
		unlocks[t.URL] = unlock

		hash, isAuthorised := authorisedHashes[t.URL]
		_, hasRun := latest[t.URL]
		promptHashMatches := isAuthorised && hash == plan.Hash(plan.Compose(pt))
		candidates = append(candidates, plan.LaunchCandidate{
			URL:               t.URL,
			Unlock:            unlock,
			Authorised:        isAuthorised,
			PromptHashMatches: promptHashMatches,
			HasRun:            hasRun,
			ConflictedBase:    conflictedBase(pt, byURL, unlock, stacking[t.Repo], obs),
		})
	}

	toLaunch := plan.LaunchPlan(candidates, currentlyRunning(latest), l.cfg.MaxAgents)
	if len(toLaunch) == 0 {
		return nil
	}

	byTicket := ticketsByURL(tickets)
	for _, ticketURL := range toLaunch {
		ticket := byTicket[ticketURL]
		spec := launchSpec{
			ticket:     ticket,
			baseBranch: unlocks[ticketURL].BaseBranch,
			promptHash: authorisedHashes[ticketURL],
			repoPath:   repoPaths[ticket.Repo],
		}
		if err := l.cutAndSpawn(ctx, spec); err != nil {
			return err
		}
	}
	return nil
}

// currentlyRunning counts tickets with a live-or-undisposed run: the slots LaunchPlan's max_agents
// cap must subtract before deciding how many more to start this tick.
func currentlyRunning(latest map[string]RunSummary) int {
	n := 0
	for _, s := range latest {
		if s.Pgid != nil && !s.HasOutcome {
			n++
		}
	}
	return n
}

// tickCheckingWaits bumps every ticket's checking-wait tick count by one. It runs only after a
// successful observe (RunOnce returns before reaching it otherwise), which is what makes the
// count track ticks whose observe phase succeeded and never wall clock (docs/designs/command-centre-design.md
// § 11 inv. 11) — internal/verdict.Input.Now is derived from it, not l.now().
func (l *Loop) tickCheckingWaits(ctx context.Context) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}
	urls := make([]string, len(tickets))
	for i, t := range tickets {
		urls[i] = t.URL
	}
	return l.store.IncrementCheckingTicks(ctx, urls)
}

func ticketsByURL(tickets []Ticket) map[string]Ticket {
	byURL := make(map[string]Ticket, len(tickets))
	for _, t := range tickets {
		byURL[t.URL] = t
	}
	return byURL
}

// launchSpec is one candidate's cut-and-spawn inputs, gathered so cutAndSpawn's own body reads
// as the spawn sequence rather than a map-lookup dance.
type launchSpec struct {
	ticket     Ticket
	baseBranch string
	promptHash string
	repoPath   string
}

// cutAndSpawn is the spawn sequence (docs/prds/prd-command-centre.md § A run) for a ticket with no
// worktree yet: cut, then hand off to spawnRun. tp new failing is `cut failed`, not a crash —
// one INSERT, no pgid, ever, and move on to the next candidate rather than failing the tick.
func (l *Loop) cutAndSpawn(ctx context.Context, spec launchSpec) error {
	branch := spec.ticket.Branch
	baseRef := "origin/" + spec.baseBranch

	if err := tp.New(ctx, spec.repoPath, branch, baseRef); err != nil {
		_, insertErr := l.store.InsertCutFailedRun(ctx, spec.ticket.URL, spec.promptHash, l.now())
		return insertErr
	}

	baselineSHA, err := BranchTip(ctx, spec.repoPath, branch)
	if err != nil {
		return fmt.Errorf("read baseline for %s: %w", spec.ticket.URL, err)
	}

	worktrees, err := Worktrees(ctx, spec.repoPath)
	if err != nil {
		return fmt.Errorf("list worktrees after cutting %s: %w", branch, err)
	}
	worktreePath, ok := worktrees[branch]
	if !ok {
		return fmt.Errorf("tp new %s reported success but git worktree list does not show it", branch)
	}

	return l.spawnRun(ctx, spec.ticket, worktreePath, baselineSHA, spec.promptHash, "", runKindAgent, "", "")
}

// spawnRun is the part of the spawn sequence that is identical whether the worktree was just
// cut (cutAndSpawn) or already existed (verbs.go's re-run, resolveOne and followUpOne): reserve
// a run skeleton to get a run id, then name the prompt file and the log file after it (they
// cannot be named before it exists), spawn, and record the process's identity in the one UPDATE
// that is its only record.
//
// A failure to spawn is recorded as `failed` on the reserved row — the process never existed,
// so there is nothing to reap. On success, nothing may run between Spawn returning and the
// RecordSpawn call below: a crash in that gap is the one known, unclosed race in this design
// (see the PR description).
func (l *Loop) spawnRun(
	ctx context.Context, ticket Ticket, worktreePath, baselineSHA, promptHash, oldPromptPath, kind string,
	followUpText, ciLogSection string,
) error {
	var prompt string
	switch kind {
	case runKindResolve:
		prompt = plan.ComposeResolve(planTicket(ticket))
	case runKindFollowUp:
		prompt = plan.ComposeFollowUp(followUpText, ciLogSection)
	default:
		prompt = plan.Compose(planTicket(ticket))
		if ticket.Body != "" {
			prompt += "\n\n## Ticket\n\n" + ticket.Body
		}
	}

	runID, err := l.store.InsertRunSkeleton(ctx, ticket.URL, kind, baselineSHA, promptHash)
	if err != nil {
		return err
	}

	promptPath := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.prompt", runID))
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		return fmt.Errorf("write prompt for run %d: %w", runID, err)
	}

	spawnPrompt := prompt
	if oldPromptPath != "" {
		preamble, err := l.reRunDiffPreamble(ctx, ticket, oldPromptPath, prompt, runID)
		if err != nil {
			return err
		}
		if preamble != "" {
			spawnPrompt = "The previous run's prompt differed from this one:\n\n" + preamble + "\n\n" + prompt
		}
	}

	logPath := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.jsonl", runID))
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("open log for run %d: %w", runID, err)
	}
	defer func() { _ = logFile.Close() }()

	// The single-shot warning is implement-only: resolve and follow-up are short, scripted
	// skill invocations that never spawn a subagent of their own, and resolve's own prompt
	// already tells it not to commit -- directly contradicting the warning's "commit before it
	// ends".
	var systemPromptPath string
	if kind == runKindAgent {
		systemPromptPath = l.ws.SystemPromptPath
	}

	spawnCfg := SpawnConfig{
		AgentCommand:     l.cfg.AgentCommand,
		WorktreePath:     worktreePath,
		SettingsPath:     l.ws.SettingsPath,
		SystemPromptPath: systemPromptPath,
		Prompt:           spawnPrompt,
		PromptPath:       promptPath,
		LogFile:          logFile,
	}
	result, err := l.runner.Spawn(ctx, spawnCfg)
	if err != nil {
		return l.store.RecordDisposition(ctx, runID, plan.OutcomeFailed, nil, l.now())
	}

	// Nothing may be added between here and the UPDATE below — see the doc comment above.
	pgid := result.Pid
	startedAt := l.now()
	if err := l.store.RecordSpawn(ctx, runID, pgid, startedAt, logPath); err != nil {
		return err
	}
	return l.store.AppendEvent(ctx, Event{
		At: startedAt, TicketURL: ticket.URL, Kind: eventRunLaunched,
		Detail: fmt.Sprintf("spawned pid %d in %s", pgid, worktreePath),
	})
}

func (l *Loop) reRunDiffPreamble(
	ctx context.Context, ticket Ticket, oldPromptPath, newPrompt string, runID int64,
) (string, error) {
	if _, err := os.Stat(oldPromptPath); errors.Is(err, os.ErrNotExist) {
		return "", l.store.AppendEvent(ctx, Event{
			At: l.now(), TicketURL: ticket.URL, Kind: eventReRunNoDiff,
			Detail: fmt.Sprintf("no prompt file at %s", oldPromptPath),
		})
	} else if err != nil {
		return "", fmt.Errorf("stat stored prompt %s: %w", oldPromptPath, err)
	}

	diff, err := unifiedDiff(ctx, oldPromptPath, newPrompt)
	if err != nil {
		return "", fmt.Errorf("diff re-run prompt for %s: %w", ticket.URL, err)
	}
	if diff == "" {
		return "", nil
	}

	diffPath := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.diff", runID))
	if err := os.WriteFile(diffPath, []byte(diff), 0o600); err != nil {
		return "", fmt.Errorf("write diff for run %d: %w", runID, err)
	}
	return diff, nil
}

// unifiedDiff shells out to diff(1): both GNU and BSD diff (Darwin's default) accept -u and
// --label.
func unifiedDiff(ctx context.Context, beforePath, after string) (string, error) {
	cmd := exec.CommandContext(ctx, "diff", "-u", "--label", "before", "--label", "after", beforePath, "-")
	cmd.Stdin = strings.NewReader(after)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return string(out), nil
	}
	if err != nil {
		return "", fmt.Errorf("diff %s: %w: %s", beforePath, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return "", nil
}
