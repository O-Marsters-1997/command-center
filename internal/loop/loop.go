package loop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

const killVerb = plan.VerbKill

const (
	runKindAgent    = "agent"
	runKindResolve  = plan.RunKindResolve
	runKindFollowUp = "follow_up"
)

const (
	eventRunLaunched = "run_launched"
	eventRunDisposed = "run_disposed"
)

// Loop is the reconcile loop: observe, decide, act. It is the only writer of reconciled state.
type Loop struct {
	store      *store.Store
	observe    ObserveFunc
	clock      Clock
	forge      gh.Forge
	worktrees  git.Worktrees
	runner     runner.Runner
	cfg        config.Config
	ws         config.Workspace
	trackerFor tracker.Resolver
	nudgeCh    chan struct{}
	spawned    []string
}

// NewLoop assembles the loop over an observe phase, a clock and the configuration a tick's cut
// and spawn steps need (repos, agent_command, max_agents, the state dir's runs and settings
// paths). spawner is the seam a test substitutes for real process spawning, liveness and cancel.
func NewLoop(
	store *store.Store, observe ObserveFunc, clock Clock, cfg config.Config, ws config.Workspace, spawner runner.Runner,
) *Loop {
	return &Loop{
		store: store, observe: observe, clock: clock, forge: gh.CLI{}, runner: spawner, cfg: cfg, ws: ws,
		worktrees:  git.CLI{},
		trackerFor: tracker.New,
		nudgeCh:    make(chan struct{}, 1),
	}
}

// Nudge wakes Run for one tick right now rather than at the end of store.TickPeriod. A nudge that
// finds the buffer full is dropped, not queued: the tick already in flight will pick up
// whatever intent prompted it anyway.
func (l *Loop) Nudge() {
	select {
	case l.nudgeCh <- struct{}{}:
	default:
	}
}

// SetForge replaces the real gh-backed Forge, so a test can fake GitHub in-process.
func (l *Loop) SetForge(forge gh.Forge) { l.forge = forge }

// SetWorktrees replaces the real tp-backed Worktrees, so a test can cut and remove worktrees
// without the tp binary.
func (l *Loop) SetWorktrees(worktrees git.Worktrees) { l.worktrees = worktrees }

// SetTrackerSource replaces the loop's tracker.New, so a test can drive applyImportIntents with a
// fake source rather than shelling out to gh.
func (l *Loop) SetTrackerSource(resolve tracker.Resolver) { l.trackerFor = resolve }

// RunOnce runs one tick. A failed observe records the error and leaves the last good
// observation in place rather than applying any transition, so the page's observe age
// keeps growing instead of resetting.
func (l *Loop) RunOnce(ctx context.Context) error {
	l.sweepExpiredSessions(ctx)

	if err := l.applyImportIntents(ctx); err != nil {
		return err
	}
	if err := l.applyEditTicketIntents(ctx); err != nil {
		return err
	}

	obs, err := l.observe(ctx)
	if err != nil {
		at := l.clock.Now()
		tickErr := fmt.Errorf("observe: %w", err)
		if recordErr := l.store.RecordTickError(ctx, store.TickError{At: at, Message: tickErr.Error()}); recordErr != nil {
			return recordErr
		}
		return tickErr
	}

	obs.ObservedAt = l.clock.Now()
	if obs.LocalTips == nil {
		obs.LocalTips = map[string]string{}
	}
	if obs.Runs == nil {
		obs.Runs = map[string]plan.RunObservation{}
	}
	if err := l.absorb(ctx, obs); err != nil {
		return err
	}
	snap, err := l.derive(ctx, obs)
	if err != nil {
		return err
	}
	return l.act(ctx, snap, obs)
}

func (l *Loop) absorb(ctx context.Context, obs plan.Observation) error {
	if err := l.tickCheckingWaits(ctx); err != nil {
		return err
	}
	if err := l.store.SaveObservation(ctx, obs); err != nil {
		return err
	}
	if err := l.store.ApplyLaunchIntents(ctx, l.clock.Now()); err != nil {
		return err
	}
	if err := l.applyCancelIntents(ctx); err != nil {
		return err
	}
	l.spawned = nil
	if err := l.applyKillIntents(ctx); err != nil {
		return err
	}
	if err := l.reconcileRuns(ctx, obs); err != nil {
		return err
	}
	if err := l.store.SaveObservation(ctx, obs); err != nil {
		return err
	}
	if err := l.recordVerdictTransitions(ctx, obs); err != nil {
		return err
	}
	return l.recordMergedEvents(ctx, obs)
}

func (l *Loop) act(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	if err := l.applyReRunIntents(ctx, snap, obs); err != nil {
		return err
	}
	if err := l.applyFollowUpIntents(ctx, obs); err != nil {
		return err
	}
	if err := l.applyAbortIntents(ctx, snap, obs); err != nil {
		return err
	}
	if err := l.resolveGeneratedConflicts(ctx, obs); err != nil {
		return err
	}
	if err := l.applyResolveIntents(ctx, obs); err != nil {
		return err
	}
	for _, url := range l.spawned {
		obs.Runs[url] = plan.RunObservation{Alive: true}
	}
	if err := l.retargetMerged(ctx, snap, obs); err != nil {
		return err
	}
	if err := l.applyRefreshIntents(ctx, snap, obs); err != nil {
		return err
	}
	rereadLocalTips(ctx, obs, repoPathsByName(l.cfg.Repos))
	if err := l.applyRetryPushIntents(ctx, snap, obs); err != nil {
		return err
	}
	if err := l.applyRemoveWorktreeIntents(ctx, snap, obs); err != nil {
		return err
	}
	if err := l.applyCommitResolutionIntents(ctx, snap, obs); err != nil {
		return err
	}
	if err := l.pushPushable(ctx, snap, obs); err != nil {
		return err
	}
	if err := l.applyDraftGate(ctx, snap); err != nil {
		return err
	}
	return l.launchEligible(ctx, snap)
}

func (l *Loop) derive(ctx context.Context, obs plan.Observation) (plan.Snapshot, error) {
	in, err := l.store.PlanInput(ctx)
	if err != nil {
		return plan.Snapshot{}, err
	}
	in.Now = l.clock.Now()
	in.Obs = obs
	return l.cfg.PlanRules().Derive(in), nil
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
		case <-l.clock.After(store.TickPeriod):
		case <-l.nudgeCh:
		}
	}
}

func (l *Loop) sweepExpiredSessions(ctx context.Context) {
	if _, err := l.store.DeleteExpiredSessions(ctx, l.clock.Now()); err != nil {
		log.Printf("sweep expired sessions: %v", err)
	}
}

func (l *Loop) applyImportIntents(ctx context.Context) error {
	intents, err := l.store.PendingVerbIntents(ctx, importVerb)
	if err != nil {
		return err
	}
	if len(intents) == 0 {
		return nil
	}

	now := l.clock.Now()
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
	var matched []store.ImportedTicket
	for _, repo := range l.cfg.Repos {
		src, ok, err := tracker.ForRemote(l.trackerFor, tracker.Kind(repo.Tracker), repo.Remote)
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
			matched = append(matched, store.ImportedTicket{Ticket: t, Repo: repo.Name, Source: repo.Tracker})
		}
	}

	err := l.store.ImportTickets(ctx, feature, matched, l.clock.Now())
	var conflict *store.FeatureConflictError
	if errors.As(err, &conflict) {
		return l.store.RecordImportRefusal(ctx, feature, conflict, l.clock.Now())
	}
	var closure *store.FeatureClosureError
	if errors.As(err, &closure) {
		return l.store.RecordImportRefusal(ctx, feature, closure, l.clock.Now())
	}
	return err
}

func (l *Loop) applyEditTicketIntents(ctx context.Context) error {
	intents, err := l.store.PendingEditTicketIntents(ctx)
	if err != nil {
		return err
	}

	now := l.clock.Now()
	for _, intent := range intents {
		err := l.store.EditTicket(ctx, intent.TicketID, intent.Branch, intent.BlockedBy)
		var closure *store.FeatureClosureError
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

	now := l.clock.Now()
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

func (l *Loop) reconcileRuns(ctx context.Context, obs plan.Observation) error {
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

	now := l.clock.Now()
	for _, run := range pending {
		alive, err := l.runner.Liveness(run.Pgid, run.ProcStartedAt, now)
		if err != nil {
			return fmt.Errorf("liveness for run %d: %w", run.ID, err)
		}
		obs.Runs[run.TicketID] = plan.RunObservation{Alive: alive}
		if alive {
			continue
		}
		if err := l.disposeRun(ctx, run, byTicket[run.TicketID], obs, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) disposeRun(
	ctx context.Context, run store.PendingRun, ticket store.Ticket, obs plan.Observation, now time.Time,
) error {
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
	metrics := l.parseRunMetrics(run.LogPath)
	if err := l.store.RecordDisposition(ctx, run.ID, outcome, exitCode, now, metrics); err != nil {
		return fmt.Errorf("record disposition for run %d: %w", run.ID, err)
	}
	readings := l.parseReadings(run.LogPath)
	if err := l.store.RecordReadingsAndIntervals(ctx, readings, l.cfg.ClaudeProjectsDir); err != nil {
		return fmt.Errorf("record readings for run %d: %w", run.ID, err)
	}
	return l.store.AppendEvent(ctx, store.Event{
		At: now, TicketURL: ticket.URL, Kind: eventRunDisposed, Detail: outcome.String(),
	})
}

func (l *Loop) parseRunMetrics(logPath string) *agentlog.RunMetrics {
	if logPath == "" {
		return nil
	}
	metrics, err := agentlog.ParseMetrics(logPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("parse run metrics %s: %v", logPath, err)
		}
		return nil
	}
	return &metrics
}

func (l *Loop) parseReadings(logPath string) []agentlog.Reading {
	if logPath == "" {
		return nil
	}
	readings, err := agentlog.ParseReadings(logPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("parse run readings %s: %v", logPath, err)
		}
		return nil
	}
	return readings
}

func (l *Loop) commitsSinceBaseline(
	ctx context.Context, ticket store.Ticket, obs plan.Observation, baselineSHA string,
) (int, error) {
	if worktreePath := obs.Worktrees[branchKey(ticket.Repo, ticket.Branch)]; worktreePath != "" {
		return git.CommitsSince(ctx, worktreePath, baselineSHA, "HEAD")
	}
	tip, ok := obs.BranchTips[branchKey(ticket.Repo, ticket.Branch)]
	if !ok {
		return 0, nil
	}
	repoPath := repoPathsByName(l.cfg.Repos)[ticket.Repo]
	return git.CommitsSince(ctx, repoPath, baselineSHA, tip)
}

func (l *Loop) launchEligible(ctx context.Context, snap plan.Snapshot) error {
	toLaunch := snap.LaunchAfter(len(l.spawned))
	if len(toLaunch) == 0 {
		return nil
	}

	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return err
	}
	byTicket := ticketsByURL(tickets)
	repoPaths := repoPathsByName(l.cfg.Repos)
	for _, ticketURL := range toLaunch {
		entry, _ := snap.Entry(ticketURL)
		ticket := byTicket[ticketURL]
		spec := launchSpec{
			ticket:     ticket,
			baseBranch: entry.Unlock.BaseBranch,
			promptHash: entry.PromptHash,
			repoPath:   repoPaths[ticket.Repo],
		}
		if err := l.cutAndSpawn(ctx, spec); err != nil {
			return err
		}
	}
	return nil
}

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

func ticketsByURL(tickets []store.Ticket) map[string]store.Ticket {
	byURL := make(map[string]store.Ticket, len(tickets))
	for _, t := range tickets {
		byURL[t.URL] = t
	}
	return byURL
}

type launchSpec struct {
	ticket     store.Ticket
	baseBranch string
	promptHash string
	repoPath   string
}

func (l *Loop) cutAndSpawn(ctx context.Context, spec launchSpec) error {
	branch := spec.ticket.Branch
	baseRef := "origin/" + spec.baseBranch

	if err := l.worktrees.New(ctx, spec.repoPath, branch, baseRef); err != nil {
		_, insertErr := l.store.InsertCutFailedRun(ctx, spec.ticket.URL, spec.promptHash, l.clock.Now())
		return insertErr
	}

	baselineSHA, err := git.BranchTip(ctx, spec.repoPath, branch)
	if err != nil {
		return fmt.Errorf("read baseline for %s: %w", spec.ticket.URL, err)
	}

	worktrees, err := git.WorktreePaths(ctx, spec.repoPath)
	if err != nil {
		return fmt.Errorf("list worktrees after cutting %s: %w", branch, err)
	}
	worktreePath, ok := worktrees[branch]
	if !ok {
		return fmt.Errorf("tp new %s reported success but git worktree list does not show it", branch)
	}

	return l.spawnRun(ctx, spec.ticket, worktreePath, baselineSHA, spec.promptHash, runKindAgent, "")
}

func (l *Loop) spawnRun(
	ctx context.Context, ticket store.Ticket, worktreePath, baselineSHA, promptHash, kind string,
	followUpText string,
) error {
	var prompt string
	switch kind {
	case runKindResolve:
		prompt = plan.ComposeResolve(ticket.Plan())
	case runKindFollowUp:
		prompt = plan.ComposeFollowUp(followUpText)
	default:
		prompt = plan.Compose(ticket.Plan())
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

	logPath := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.jsonl", runID))
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("open log for run %d: %w", runID, err)
	}
	defer func() { _ = logFile.Close() }()

	var systemPromptPath, agentsPath string
	if kind == runKindAgent {
		systemPromptPath = l.ws.SystemPromptPath
		agentsPath = l.ws.AgentsPath
	}

	spawnCfg := runner.SpawnConfig{
		AgentCommand:     l.cfg.AgentCommand,
		WorktreePath:     worktreePath,
		SettingsPath:     l.ws.SettingsPath,
		SystemPromptPath: systemPromptPath,
		AgentsPath:       agentsPath,
		Prompt:           prompt,
		PromptPath:       promptPath,
		LogFile:          logFile,
	}
	result, err := l.runner.Spawn(ctx, spawnCfg)
	if err != nil {
		return l.store.RecordDisposition(ctx, runID, plan.OutcomeFailed, nil, l.clock.Now(), nil)
	}

	pgid := result.Pid
	startedAt := l.clock.Now()
	if err := l.store.RecordSpawn(ctx, runID, pgid, startedAt, logPath); err != nil {
		return err
	}
	l.spawned = append(l.spawned, ticket.URL)
	return l.store.AppendEvent(ctx, store.Event{
		At: startedAt, TicketURL: ticket.URL, Kind: eventRunLaunched,
		Detail: fmt.Sprintf("spawned pid %d in %s", pgid, worktreePath),
	})
}
