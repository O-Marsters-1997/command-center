// Package loop is the Command Centre's imperative shell: the tick loop and its steps.
// The pure decisions live in internal/plan; gh's JSON shape lives in internal/gh.
package loop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

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
// and spawn steps need (data_dir, agent_command, max_agents, the state dir's runs and settings
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

func (l *Loop) checkout(repo string) string { return config.CheckoutPath(l.cfg.DataDir, repo) }

func (l *Loop) event(ctx context.Context, ticketURL, kind, detail string) error {
	return l.store.AppendEvent(ctx, store.Event{At: l.clock.Now(), TicketURL: ticketURL, Kind: kind, Detail: detail})
}

func (l *Loop) eachIntent(ctx context.Context, verb string, fn func(store.VerbIntent) error) error {
	intents, err := l.store.PendingVerbIntents(ctx, verb)
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if err := fn(intent); err != nil {
			return err
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, l.clock.Now()); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) ticket(ctx context.Context, url string) (store.Ticket, bool, error) {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return store.Ticket{}, false, err
	}
	i := slices.IndexFunc(tickets, func(t store.Ticket) bool { return t.URL == url })
	if i < 0 {
		return store.Ticket{}, false, nil
	}
	return tickets[i], true, nil
}

func runSteps(steps ...func() error) error {
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
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
	if err := l.recordSettingsErrors(ctx, obs); err != nil {
		return err
	}
	snap, err := l.derive(ctx, obs)
	if err != nil {
		return err
	}
	return l.act(ctx, snap.Skipping(skippedRepos(obs)), obs)
}

func (l *Loop) absorb(ctx context.Context, obs plan.Observation) error {
	return runSteps(
		func() error { return l.tickCheckingWaits(ctx) },
		func() error { return l.store.SaveObservation(ctx, obs) },
		func() error { return l.store.ApplyLaunchIntents(ctx, l.clock.Now()) },
		func() error { return l.applyCancelIntents(ctx) },
		func() error { l.spawned = nil; return nil },
		func() error { return l.applyKillIntents(ctx) },
		func() error { return l.reconcileRuns(ctx, obs) },
		func() error { return l.store.SaveObservation(ctx, obs) },
		func() error { return l.recordVerdictTransitions(ctx, obs) },
		func() error { return l.recordMergedEvents(ctx, obs) },
	)
}

func (l *Loop) act(ctx context.Context, snap plan.Snapshot, obs plan.Observation) error {
	return runSteps(
		func() error { return l.applyReRunIntents(ctx, snap, obs) },
		func() error { return l.applyFollowUpIntents(ctx, obs) },
		func() error { return l.applyAbortIntents(ctx, snap, obs) },
		func() error { return l.applyResolveIntents(ctx, obs) },
		func() error { l.markSpawnedAlive(obs); return nil },
		func() error { return l.retargetMerged(ctx, snap, obs) },
		func() error { return l.applyRefreshIntents(ctx, snap, obs) },
		func() error { l.rereadLocalTips(ctx, obs); return nil },
		func() error { return l.applyRetryPushIntents(ctx, snap, obs) },
		func() error { return l.applyRemoveWorktreeIntents(ctx, snap, obs) },
		func() error { return l.applyCommitResolutionIntents(ctx, snap, obs) },
		func() error { return l.pushPushable(ctx, snap, obs) },
		func() error { return l.applyDraftGate(ctx, snap) },
		func() error { return l.launchEligible(ctx, snap) },
	)
}

func (l *Loop) markSpawnedAlive(obs plan.Observation) {
	for _, url := range l.spawned {
		obs.Runs[url] = plan.RunObservation{Alive: true}
	}
}

func (l *Loop) derive(ctx context.Context, obs plan.Observation) (plan.Snapshot, error) {
	in, err := l.store.PlanInput(ctx)
	if err != nil {
		return plan.Snapshot{}, err
	}
	in.Now = l.clock.Now()
	in.Obs = obs
	return l.rules(obs).Derive(in), nil
}

func (l *Loop) rules(obs plan.Observation) plan.Rules {
	return plan.RulesFor(plan.Daemon{MaxAgents: l.cfg.MaxAgents, SpendLimit5h: l.cfg.SpendLimit5h}, obs)
}

func (l *Loop) recordSettingsErrors(ctx context.Context, obs plan.Observation) error {
	if len(obs.SettingsErrors) == 0 {
		return nil
	}
	repos := slices.Sorted(maps.Keys(obs.SettingsErrors))
	parts := make([]string, 0, len(repos))
	for _, repo := range repos {
		parts = append(parts, fmt.Sprintf("settings for %s: %s", repo, obs.SettingsErrors[repo]))
	}
	message := strings.Join(parts, "; ")
	last, found, err := l.store.LastError(ctx)
	if err != nil {
		return err
	}
	if found && last.Message == message {
		return nil
	}
	return l.store.RecordTickError(ctx, store.TickError{At: l.clock.Now(), Message: message})
}

func skippedRepos(obs plan.Observation) map[string]bool {
	skipped := make(map[string]bool, len(obs.SettingsErrors))
	for repo := range obs.SettingsErrors {
		skipped[repo] = true
	}
	return skipped
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
	return l.eachIntent(ctx, store.ImportVerb, func(intent store.VerbIntent) error {
		return l.importFeature(ctx, intent.TicketID)
	})
}

func (l *Loop) importFeature(ctx context.Context, feature string) error {
	lastObs, _, err := l.store.LastObservation(ctx)
	if err != nil {
		return err
	}
	repos, err := readyRepos(ctx, l.store)
	if err != nil {
		return err
	}
	var matched []store.ImportedTicket
	for _, repo := range repos {
		trackerKind := config.DefaultRepoSettings().Tracker
		if settings, ok := lastObs.Settings[repo.Name]; ok {
			trackerKind = settings.Tracker
		}
		src, ok, err := tracker.ForRemote(l.trackerFor, tracker.Kind(trackerKind), repo.Remote)
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
			matched = append(matched, store.ImportedTicket{Ticket: t, Repo: repo.Name, Source: trackerKind})
		}
	}

	err = l.store.ImportTickets(ctx, feature, matched, l.clock.Now())
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
	return l.eachIntent(ctx, plan.VerbKill, func(intent store.VerbIntent) error {
		latest, err := l.store.LatestRunsByTicket(ctx)
		if err != nil {
			return err
		}
		if run, ok := latest[intent.TicketID]; ok && run.Pgid != nil && !run.HasOutcome {
			if err := l.runner.Cancel(*run.Pgid); err != nil {
				return fmt.Errorf("cancel %s (pgid %d): %w", intent.TicketID, *run.Pgid, err)
			}
		}
		return nil
	})
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
		if err := l.disposeRun(ctx, run, byTicket[run.TicketID], obs); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) disposeRun(
	ctx context.Context, run store.PendingRun, ticket store.Ticket, obs plan.Observation,
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
	var metrics *agentlog.RunMetrics
	if m, ok := parseLog(run.LogPath, "run metrics", agentlog.ParseMetrics); ok {
		metrics = &m
	}
	if err := l.store.RecordDisposition(ctx, run.ID, outcome, exitCode, l.clock.Now(), metrics); err != nil {
		return fmt.Errorf("record disposition for run %d: %w", run.ID, err)
	}
	readings, _ := parseLog(run.LogPath, "run readings", agentlog.ParseReadings)
	if err := l.store.RecordReadingsAndIntervals(ctx, readings, l.cfg.ClaudeProjectsDir); err != nil {
		return fmt.Errorf("record readings for run %d: %w", run.ID, err)
	}
	return l.event(ctx, ticket.URL, eventRunDisposed, outcome.String())
}

func parseLog[T any](logPath, what string, parse func(string) (T, error)) (T, bool) {
	var zero T
	if logPath == "" {
		return zero, false
	}
	v, err := parse(logPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("parse %s %s: %v", what, logPath, err)
		}
		return zero, false
	}
	return v, true
}

func (l *Loop) commitsSinceBaseline(
	ctx context.Context, ticket store.Ticket, obs plan.Observation, baselineSHA string,
) (int, error) {
	if worktreePath := obs.Worktrees[plan.BranchKey(ticket.Repo, ticket.Branch)]; worktreePath != "" {
		return git.CommitsSince(ctx, worktreePath, baselineSHA, "HEAD")
	}
	tip, ok := obs.BranchTips[plan.BranchKey(ticket.Repo, ticket.Branch)]
	if !ok {
		return 0, nil
	}
	repoPath := l.checkout(ticket.Repo)
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
	for _, ticketURL := range toLaunch {
		entry, _ := snap.Entry(ticketURL)
		ticket := byTicket[ticketURL]
		if err := l.cutAndSpawn(ctx, ticket, entry.Unlock.BaseBranch, entry.PromptHash); err != nil {
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

func (l *Loop) cutAndSpawn(ctx context.Context, ticket store.Ticket, baseBranch, promptHash string) error {
	branch := ticket.Branch
	repoPath := l.checkout(ticket.Repo)

	if err := l.worktrees.New(ctx, repoPath, branch, "origin/"+baseBranch); err != nil {
		_, insertErr := l.store.InsertCutFailedRun(ctx, ticket.URL, promptHash, l.clock.Now())
		return insertErr
	}

	baselineSHA, err := git.BranchTip(ctx, repoPath, branch)
	if err != nil {
		return fmt.Errorf("read baseline for %s: %w", ticket.URL, err)
	}

	worktrees, err := git.WorktreePaths(ctx, repoPath)
	if err != nil {
		return fmt.Errorf("list worktrees after cutting %s: %w", branch, err)
	}
	worktreePath, ok := worktrees[branch]
	if !ok {
		return fmt.Errorf("tp new %s reported success but git worktree list does not show it", branch)
	}

	return l.spawnRun(ctx, spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: baselineSHA, hash: promptHash,
		kind: runKindAgent, prompt: agentPrompt(ticket),
	})
}

func agentPrompt(ticket store.Ticket) string {
	prompt := plan.Compose(ticket.Plan())
	if ticket.Body != "" {
		prompt += "\n\n## Ticket\n\n" + ticket.Body
	}
	return prompt
}

type spawnSpec struct {
	ticket                                 store.Ticket
	worktree, baseline, hash, kind, prompt string
}

func (l *Loop) spawnRun(ctx context.Context, spec spawnSpec) error {
	ticket := spec.ticket
	runID, err := l.store.InsertRunSkeleton(ctx, ticket.URL, spec.kind, spec.baseline, spec.hash)
	if err != nil {
		return err
	}

	promptPath := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.prompt", runID))
	if err := os.WriteFile(promptPath, []byte(spec.prompt), 0o600); err != nil {
		return fmt.Errorf("write prompt for run %d: %w", runID, err)
	}

	logPath := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.jsonl", runID))
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("open log for run %d: %w", runID, err)
	}
	defer func() { _ = logFile.Close() }()

	spawnCfg := runner.SpawnConfig{
		AgentCommand: l.cfg.AgentCommand,
		WorktreePath: spec.worktree,
		SettingsPath: l.ws.SettingsPath,
		Prompt:       spec.prompt,
		PromptPath:   promptPath,
		LogFile:      logFile,
	}
	if spec.kind == runKindAgent {
		spawnCfg.SystemPromptPath = l.ws.SystemPromptPath
		spawnCfg.AgentsPath = l.ws.AgentsPath
	}
	result, err := l.runner.Spawn(ctx, spawnCfg)
	if err != nil {
		return l.store.RecordDisposition(ctx, runID, plan.OutcomeFailed, nil, l.clock.Now(), nil)
	}

	if err := l.store.RecordSpawn(ctx, runID, result.Pid, l.clock.Now(), logPath); err != nil {
		return err
	}
	l.spawned = append(l.spawned, ticket.URL)
	return l.event(ctx, ticket.URL, eventRunLaunched, fmt.Sprintf("spawned pid %d in %s", result.Pid, spec.worktree))
}
