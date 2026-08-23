package cc

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/tp"
)

// tickPeriod is the sleep *after* work: ticks never overlap, and the loop never branches on
// why it woke.
const tickPeriod = 15 * time.Second

// killVerb is the only verb this phase implements; the others in the design's route table
// (re-run, retry-push, close-pr, remove-worktree) belong to later phases.
const killVerb = "kill"

// TickError is the last failed tick, rendered on the page with its age.
type TickError struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

// Loop is the reconcile loop: observe, decide, act. It is the only writer of the database
// (inv. 9).
type Loop struct {
	store   *Store
	observe ObserveFunc
	now     func() time.Time
	runner  Runner
	cfg     Config
	ws      Workspace
}

// NewLoop assembles the loop over an observe phase, a clock and the configuration a tick's cut
// and spawn steps need (repos, agent_command, max_agents, the state dir's runs and settings
// paths). runner is the seam a test substitutes for real process spawning, liveness and cancel.
func NewLoop(store *Store, observe ObserveFunc, now func() time.Time, cfg Config, ws Workspace, runner Runner) *Loop {
	return &Loop{store: store, observe: observe, now: now, runner: runner, cfg: cfg, ws: ws}
}

// RunOnce runs one tick. A failed observe applies no transition and launches nothing: it
// records the error and leaves the last good observation in place, so the page's observe age
// keeps growing (inv. 10). Only a successful observe goes on to apply queued launch intents,
// consume kill requests, reconcile every run's liveness and disposition, and cut and spawn
// whatever plan.LaunchPlan selects — in that order, every tick, restart or not (§ Crash
// recovery: there is no separate recovery code path, this is the same tick).
func (l *Loop) RunOnce(ctx context.Context) error {
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
	if err := l.store.SaveObservation(ctx, obs); err != nil {
		return err
	}
	if err := l.store.ApplyLaunchIntents(ctx, l.now()); err != nil {
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
		}
	}
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

	latest, err := l.store.LatestRunsByTask(ctx)
	if err != nil {
		return err
	}

	now := l.now()
	for _, intent := range intents {
		if run, ok := latest[intent.TaskID]; ok && run.Pgid != nil && !run.HasOutcome {
			if err := l.runner.Cancel(*run.Pgid); err != nil {
				return fmt.Errorf("cancel %s (pgid %d): %w", intent.TaskID, *run.Pgid, err)
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

	tasks, err := l.store.Tasks(ctx)
	if err != nil {
		return err
	}
	byTicket := tasksByTicket(tasks)

	now := l.now()
	for _, run := range pending {
		alive, err := l.runner.Liveness(run.Pgid, run.ProcStartedAt, now)
		if err != nil {
			return fmt.Errorf("liveness for run %d: %w", run.ID, err)
		}
		obs.Runs[run.TaskID] = RunObservation{Alive: alive}
		if alive {
			continue
		}
		if err := l.disposeRun(ctx, run, byTicket[run.TaskID], obs, now); err != nil {
			return err
		}
	}
	return nil
}

// disposeRun computes and records one dead run's outcome (docs/prd-command-centre.md § A run):
// commits after its own baseline decide push vs failed, never a missing event (inv. 7).
func (l *Loop) disposeRun(ctx context.Context, run PendingRun, task Task, obs Observation, now time.Time) error {
	commits := 0
	if worktreePath := obs.Worktrees[task.Branch]; worktreePath != "" && run.BaselineSHA != "" {
		var err error
		commits, err = CommitsSince(ctx, worktreePath, run.BaselineSHA)
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
	return nil
}

// launchEligible is job 3 of the tick: plan.LaunchPlan picks the tickets to cut and spawn this
// tick, under max_agents applied globally over every repo's unlock results.
func (l *Loop) launchEligible(ctx context.Context, obs Observation) error {
	tasks, err := l.store.Tasks(ctx)
	if err != nil {
		return err
	}
	authorisedHashes, err := l.store.ActiveLaunchHashes(ctx)
	if err != nil {
		return err
	}
	latest, err := l.store.LatestRunsByTask(ctx)
	if err != nil {
		return err
	}

	stacking := stackingByRepo(l.cfg.Repos)
	byURL := planTasksByURL(tasks)
	prs := prsByBranch(obs)

	candidates := make([]plan.LaunchCandidate, 0, len(tasks))
	unlocks := make(map[string]plan.Unlock, len(tasks))
	for _, t := range tasks {
		pt := planTask(t)
		unlock := plan.Unlocked(pt, byURL, prs, stacking[t.Repo])
		unlocks[t.TicketURL] = unlock

		hash, isAuthorised := authorisedHashes[t.TicketURL]
		_, hasRun := latest[t.TicketURL]
		candidates = append(candidates, plan.LaunchCandidate{
			TicketURL:         t.TicketURL,
			Unlock:            unlock,
			Authorised:        isAuthorised,
			PromptHashMatches: isAuthorised && hash == plan.Hash(plan.Compose(pt, nil)),
			HasRun:            hasRun,
		})
	}

	toLaunch := plan.LaunchPlan(candidates, currentlyRunning(latest), l.cfg.MaxAgents)
	if len(toLaunch) == 0 {
		return nil
	}

	byTicket := tasksByTicket(tasks)
	repoPaths := repoPathsByName(l.ws.Root, l.cfg.Repos)
	for _, ticketURL := range toLaunch {
		task := byTicket[ticketURL]
		spec := launchSpec{
			task:       task,
			baseBranch: unlocks[ticketURL].BaseBranch,
			promptHash: authorisedHashes[ticketURL],
			repoPath:   repoPaths[task.Repo],
		}
		if err := l.cutAndSpawn(ctx, spec); err != nil {
			return err
		}
	}
	return nil
}

// currentlyRunning counts tasks with a live-or-undisposed run: the slots LaunchPlan's max_agents
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

func tasksByTicket(tasks []Task) map[string]Task {
	byTicket := make(map[string]Task, len(tasks))
	for _, t := range tasks {
		byTicket[t.TicketURL] = t
	}
	return byTicket
}

// launchSpec is one candidate's cut-and-spawn inputs, gathered so cutAndSpawn's own body reads
// as the spawn sequence rather than a map-lookup dance.
type launchSpec struct {
	task       Task
	baseBranch string
	promptHash string
	repoPath   string
}

// cutAndSpawn is the spawn sequence (docs/prd-command-centre.md § A run), in the order the
// design calls out as load-bearing:
//
//  1. Cut. tp new failing is `cut failed`, not a crash — one INSERT, no pgid, ever, and move on
//     to the next candidate rather than failing the tick.
//  2. Read the baseline: the branch tip at the moment this run launches.
//  3. Reserve a run skeleton to get a run id, then name the prompt file and the log file after
//     it (they cannot be named before it exists).
//  4. Spawn. A failure here is recorded as `failed` on the reserved row — the process never
//     existed, so there is nothing to reap.
//  5. Immediately after Spawn returns, with nothing else in between: capture the pid and this
//     instant as the process start time, and write both in the one UPDATE that is the only
//     record of this process's identity. No logging, no next-candidate work, between spawn and
//     that write — a crash in that gap is the one known, unclosed race in this design (see the
//     PR description).
func (l *Loop) cutAndSpawn(ctx context.Context, spec launchSpec) error {
	branch := spec.task.Branch
	baseRef := "origin/" + spec.baseBranch

	if err := tp.New(ctx, spec.repoPath, branch, baseRef); err != nil {
		_, insertErr := l.store.InsertCutFailedRun(ctx, spec.task.TicketURL, spec.promptHash, l.now())
		return insertErr
	}

	baselineSHA, err := BranchTip(ctx, spec.repoPath, branch)
	if err != nil {
		return fmt.Errorf("read baseline for %s: %w", spec.task.TicketURL, err)
	}

	runID, err := l.store.InsertRunSkeleton(ctx, spec.task.TicketURL, "agent", baselineSHA, spec.promptHash)
	if err != nil {
		return err
	}

	promptPath := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.prompt", runID))
	prompt := plan.Compose(planTask(spec.task), nil)
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		return fmt.Errorf("write prompt for run %d: %w", runID, err)
	}

	logPath := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.jsonl", runID))
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("open log for run %d: %w", runID, err)
	}
	defer func() { _ = logFile.Close() }()

	worktrees, err := Worktrees(ctx, spec.repoPath)
	if err != nil {
		return fmt.Errorf("list worktrees after cutting %s: %w", branch, err)
	}
	worktreePath, ok := worktrees[branch]
	if !ok {
		return fmt.Errorf("tp new %s reported success but git worktree list does not show it", branch)
	}

	spawnCfg := SpawnConfig{
		AgentCommand: l.cfg.AgentCommand,
		WorktreePath: worktreePath,
		SettingsPath: l.ws.SettingsPath,
		PromptPath:   promptPath,
		LogFile:      logFile,
	}
	result, err := l.runner.Spawn(ctx, spawnCfg)
	if err != nil {
		return l.store.RecordDisposition(ctx, runID, plan.OutcomeFailed, nil, l.now())
	}

	// Nothing may be added between here and the UPDATE below — see the doc comment above.
	pgid := result.Pid
	startedAt := l.now()
	return l.store.RecordSpawn(ctx, runID, pgid, startedAt, logPath)
}
