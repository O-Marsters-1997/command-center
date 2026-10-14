// Package loop is the Command Centre's imperative shell: the tick loop and its steps.
// The pure decisions live in internal/plan; gh's JSON shape lives in internal/gh.
package loop

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
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
	runKindFollowUp = "follow_up"
)

const (
	eventRunLaunched = "run_launched"
	eventRunDisposed = "run_disposed"
	eventCutFailed   = "cut_failed"
	eventSpawnFailed = "spawn_failed"
)

const AgentModel = "claude-sonnet-5-5"

// Loop is the reconcile loop: observe, decide, act. It is the only writer of reconciled state.
type Loop struct {
	logger     *slog.Logger
	store      *store.Store
	observe    ObserveFunc
	clock      Clock
	forge      gh.Forge
	worktrees  git.Worktrees
	validate   ValidateFunc
	remoteOf   RemoteFunc
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
	logger *slog.Logger, store *store.Store, observe ObserveFunc, clock Clock, cfg config.Config,
	ws config.Workspace, spawner runner.Runner,
) *Loop {
	return &Loop{
		logger: logger, store: store, observe: observe, clock: clock, forge: gh.CLI{}, runner: spawner, cfg: cfg, ws: ws,
		worktrees:  git.CLI{},
		validate:   ValidateRepo,
		remoteOf:   git.RepoRemote,
		trackerFor: tracker.New,
		nudgeCh:    make(chan struct{}, 1),
	}
}

func (l *Loop) checkout(repo string) string { return config.CheckoutPath(l.cfg.DataDir, repo) }

func (l *Loop) event(ctx context.Context, ticketURL, kind, detail string) error {
	return l.store.AppendEvent(ctx, store.Event{At: l.clock.Now(), TicketURL: ticketURL, Kind: kind, Detail: detail})
}

var errIntentPending = errors.New("intent stays pending")

func (l *Loop) eachIntent(ctx context.Context, verb string, fn func(store.VerbIntent) error) error {
	intents, err := l.store.PendingVerbIntents(ctx, verb)
	if err != nil {
		return err
	}
	for _, intent := range intents {
		if err := fn(intent); errors.Is(err, errIntentPending) {
			continue
		} else if err != nil {
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

// SetValidator replaces the gh- and git-backed repo validation, so a test or the e2e build can
// settle a cloning repo without GitHub.
func (l *Loop) SetValidator(validate ValidateFunc) { l.validate = validate }

// SetRemoteSource replaces the gh-backed remote lookup Track makes, so a test can track a repo
// without GitHub.
func (l *Loop) SetRemoteSource(remoteOf RemoteFunc) { l.remoteOf = remoteOf }

// SetTrackerSource replaces the loop's tracker.New, so a test can drive applyImportIntents with a
// fake source rather than shelling out to gh.
func (l *Loop) SetTrackerSource(resolve tracker.Resolver) { l.trackerFor = resolve }

// RunOnce runs one tick. A failed observe records the error and leaves the last good
// observation in place rather than applying any transition, so the page's observe age
// keeps growing instead of resetting.
func (l *Loop) RunOnce(ctx context.Context) error {
	if err := l.store.PruneHistory(ctx, l.clock.Now()); err != nil {
		l.logger.Warn("prune history", "err", err)
	}
	if err := l.applyTrackIntents(ctx); err != nil {
		return err
	}
	if err := l.validateCloningRepos(ctx); err != nil {
		return err
	}
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
	if err := l.recordRepoProblems(ctx, obs); err != nil {
		return err
	}
	skipped, err := l.skippedRepos(ctx, obs)
	if err != nil {
		return err
	}
	snap, err := l.derive(ctx, obs)
	if err != nil {
		return err
	}
	return l.act(ctx, snap.Skipping(skipped), obs)
}

func (l *Loop) absorb(ctx context.Context, obs plan.Observation) error {
	prev, _, err := l.store.LastObservation(ctx)
	if err != nil {
		return err
	}
	return runSteps(
		func() error { return l.recordRepoStates(ctx, obs) },
		func() error { return l.tickCheckingWaits(ctx) },
		func() error { return l.recordOpenedEvents(ctx, prev, obs) },
		func() error { return l.recordChecksLanded(ctx, prev, obs) },
		func() error { return l.store.SaveObservation(ctx, obs) },
		func() error { return l.store.ApplyLaunchIntents(ctx, l.clock.Now()) },
		func() error { return l.applyCancelIntents(ctx) },
		func() error { l.spawned = nil; return nil },
		func() error { return l.applyKillIntents(ctx) },
		func() error { return l.interruptForFollowUps(ctx) },
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
		func() error { l.markSpawnedAlive(obs); return nil },
		func() error { return l.retargetMerged(ctx, snap, obs) },
		func() error { return l.applyRefreshIntents(ctx, snap, obs) },
		func() error { l.rereadLocalTips(ctx, obs); return nil },
		func() error { return l.applyRetryPushIntents(ctx, snap, obs) },
		func() error { return l.applyRemoveWorktreeIntents(ctx, snap, obs) },
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

// recordRepoStates writes what this tick's settings read says about each repo onto its row: a
// ready repo whose file would not parse is refused, and a clean read heals that refusal alone.
func (l *Loop) recordRepoStates(ctx context.Context, obs plan.Observation) error {
	repos, err := l.store.Repos(ctx)
	if err != nil {
		return err
	}
	for _, repo := range repos {
		current := plan.RepoStatus{
			Name: repo.Name, State: string(repo.State), RefusalKind: repo.RefusalKind, Refusal: repo.Refusal,
		}
		next := plan.NextRepoStatus(current, obs)
		source, read := obs.SettingsSources[repo.Name]
		if next == current && (!read || source == repo.SettingsSource) {
			continue
		}
		repo.State, repo.RefusalKind, repo.Refusal = store.RepoState(next.State), next.RefusalKind, next.Refusal
		if read {
			repo.SettingsSource, repo.SettingsReadAt = source, l.clock.Now()
		}
		if err := l.store.SetRepoState(ctx, repo); err != nil {
			return err
		}
	}
	return nil
}

// recordRepoProblems puts every repo the loop cannot drive this tick on the masthead's last
// error: unreadable settings, a failed observe, or a refusal that waits for Track.
func (l *Loop) recordRepoProblems(ctx context.Context, obs plan.Observation) error {
	repos, err := l.store.Repos(ctx)
	if err != nil {
		return err
	}
	var parts []string
	for repo, reason := range obs.SettingsErrors {
		parts = append(parts, fmt.Sprintf("settings for %s: %s", repo, reason))
	}
	for repo, reason := range obs.RepoErrors {
		parts = append(parts, fmt.Sprintf("observe %s: %s", repo, reason))
	}
	for _, repo := range repos {
		if repo.State == store.RepoRefused && repo.RefusalKind != plan.RefusalSettingsParse {
			parts = append(parts, fmt.Sprintf("%s refused (%s): %s", repo.Name, repo.RefusalKind, repo.Refusal))
		}
	}
	if len(parts) == 0 {
		return nil
	}
	slices.Sort(parts)
	return l.recordTickError(ctx, strings.Join(parts, "; "))
}

// skippedRepos is every repo the act steps must leave alone this tick: those not ready, and those
// whose observation failed or whose settings would not read.
func (l *Loop) skippedRepos(ctx context.Context, obs plan.Observation) (map[string]bool, error) {
	repos, err := l.store.Repos(ctx)
	if err != nil {
		return nil, err
	}
	skipped := map[string]bool{}
	for _, repo := range repos {
		if repo.State != store.RepoReady {
			skipped[repo.Name] = true
		}
	}
	for repo := range obs.SettingsErrors {
		skipped[repo] = true
	}
	for repo := range obs.RepoErrors {
		skipped[repo] = true
	}
	return skipped, nil
}

// Run ticks until the context is cancelled, sleeping after each tick's work. A tick error is
// already recorded for the page, so the loop logs it and carries on.
func (l *Loop) Run(ctx context.Context) error {
	for {
		if err := l.RunOnce(ctx); err != nil {
			l.logger.Error("tick", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-l.clock.After(store.TickPeriod):
		case <-l.nudgeCh:
		}
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
	repos, err := ReadyRepos(ctx, l.store)
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

func (l *Loop) interruptForFollowUps(ctx context.Context) error {
	intents, err := l.store.PendingVerbIntents(ctx, plan.VerbFollowUp)
	if err != nil || len(intents) == 0 {
		return err
	}
	latest, err := l.store.LatestRunsByTicket(ctx)
	if err != nil {
		return err
	}
	cancelled := map[string]bool{}
	for _, intent := range intents {
		run, ok := latest[intent.TicketID]
		steerable := run.Kind == runKindAgent || run.Kind == runKindFollowUp
		if cancelled[intent.TicketID] || !ok || !steerable || run.Pgid == nil || run.HasOutcome {
			continue
		}
		cancelled[intent.TicketID] = true
		if err := l.runner.Cancel(*run.Pgid); err != nil {
			return fmt.Errorf("cancel %s (pgid %d): %w", intent.TicketID, *run.Pgid, err)
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
		if err := l.disposeRun(ctx, run, byTicket[run.TicketID], obs); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) disposeRun(
	ctx context.Context, run store.PendingRun, ticket store.Ticket, obs plan.Observation,
) error {
	outcome, err := l.outcomeFor(ctx, run, ticket, obs)
	if err != nil {
		return err
	}

	var exitCode *int
	if code, ok := l.runner.Reap(run.Pgid); ok {
		exitCode = &code
	}
	var metrics *agentlog.RunMetrics
	if m, ok := parseLog(l.logger, run.LogPath, "run metrics", agentlog.ParseMetrics); ok {
		metrics = &m
	}
	if err := l.store.RecordDisposition(ctx, run.ID, outcome, exitCode, l.clock.Now(), metrics); err != nil {
		return fmt.Errorf("record disposition for run %d: %w", run.ID, err)
	}
	readings, _ := parseLog(l.logger, run.LogPath, "run readings", agentlog.ParseReadings)
	if err := l.store.RecordReadingsAndIntervals(ctx, readings, l.cfg.ClaudeProjectsDir); err != nil {
		return fmt.Errorf("record readings for run %d: %w", run.ID, err)
	}
	return l.event(ctx, ticket.URL, eventRunDisposed, outcome.String())
}

func (l *Loop) outcomeFor(
	ctx context.Context, run store.PendingRun, ticket store.Ticket, obs plan.Observation,
) (plan.Outcome, error) {
	if run.Kind == runKindAgent || run.Kind == runKindFollowUp {
		queued, err := l.followUpQueued(ctx, run.TicketID)
		if err != nil {
			return 0, err
		}
		if queued {
			return plan.OutcomeContinued, nil
		}
	}
	question, err := l.readQuestion(run)
	if err != nil {
		return 0, err
	}
	if question != "" {
		return plan.OutcomeAsked, l.store.RecordQuestion(ctx, run.ID, question)
	}
	commits := 0
	if run.BaselineSHA != "" {
		commits, err = l.commitsSinceBaseline(ctx, ticket, obs, run.BaselineSHA)
		if err != nil {
			return 0, fmt.Errorf("commits since baseline for run %d: %w", run.ID, err)
		}
	}
	return plan.Disposition(commits), nil
}

func (l *Loop) followUpQueued(ctx context.Context, ticketURL string) (bool, error) {
	intents, err := l.store.PendingVerbIntents(ctx, plan.VerbFollowUp)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(intents, func(i store.VerbIntent) bool { return i.TicketID == ticketURL }), nil
}

const maxQuestionBytes = 4096

func (l *Loop) questionPath(runID int64) string {
	return filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.question", runID))
}

func (l *Loop) readQuestion(run store.PendingRun) (string, error) {
	if run.Kind != runKindAgent && run.Kind != runKindFollowUp {
		return "", nil
	}
	data, err := os.ReadFile(l.questionPath(run.ID))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read question for run %d: %w", run.ID, err)
	}
	if len(data) > maxQuestionBytes {
		data = data[:maxQuestionBytes]
	}
	question := strings.TrimSpace(strings.ToValidUTF8(string(data), ""))
	if question == "" {
		return "(the agent asked without writing a question)", nil
	}
	return question, nil
}

func parseLog[T any](logger *slog.Logger, logPath, what string, parse func(string) (T, error)) (T, bool) {
	var zero T
	if logPath == "" {
		return zero, false
	}
	v, err := parse(logPath)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logger.Warn("parse agent log", "what", what, "path", logPath, "err", err)
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

func (l *Loop) cutAndSpawn(
	ctx context.Context, ticket store.Ticket, baseBranch, promptHash string,
) error {
	branch := ticket.Branch
	repoPath := l.checkout(ticket.Repo)

	if err := l.worktrees.New(ctx, repoPath, branch, "origin/"+baseBranch); err != nil {
		if _, insertErr := l.store.InsertCutFailedRun(ctx, ticket.URL, promptHash, l.clock.Now()); insertErr != nil {
			return insertErr
		}
		return l.event(ctx, ticket.URL, eventCutFailed, err.Error())
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
	promptFor                              func(runID int64) string
	resumeSessionID, message               string
}

func (spec spawnSpec) session() store.RunSession {
	switch {
	case spec.resumeSessionID != "":
		return store.RunSession{JoinID: spec.resumeSessionID, Message: spec.message}
	case spec.kind == runKindAgent || spec.kind == runKindFollowUp:
		return store.RunSession{StartNew: true, Message: spec.message}
	default:
		return store.RunSession{}
	}
}

func (l *Loop) insertSkeleton(ctx context.Context, spec spawnSpec) (runID int64, sessionID string, err error) {
	return l.store.InsertSessionRunSkeleton(
		ctx, spec.ticket.URL, spec.kind, spec.baseline, spec.hash, spec.session())
}

func (l *Loop) spawnRun(ctx context.Context, spec spawnSpec) (err error) {
	ticket := spec.ticket
	runID, sessionID, err := l.insertSkeleton(ctx, spec)
	if err != nil {
		return err
	}

	promptFor := spec.promptFor
	if promptFor == nil && (spec.kind == runKindAgent || spec.kind == runKindFollowUp) {
		promptFor = func(runID int64) string { return spec.prompt + plan.ComposeAsking(l.questionPath(runID)) }
	}
	prompt := spec.prompt
	if promptFor != nil {
		prompt = promptFor(runID)
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
	defer func() {
		if closeErr := logFile.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close log for run %d: %w", runID, closeErr))
		}
	}()

	spawnCfg := runner.SpawnConfig{
		AgentCommand: l.cfg.AgentCommand,
		WorktreePath: spec.worktree,
		SettingsPath: l.ws.SettingsPath,
		Model:        AgentModel,
		Prompt:       prompt,
		PromptPath:   promptPath,
		SessionID:    sessionID,
		Resume:       spec.resumeSessionID != "",
		LogFile:      logFile,
	}
	if spec.kind == runKindAgent {
		spawnCfg.SystemPromptPath = l.ws.SystemPromptPath
	}
	result, err := l.runner.Spawn(ctx, spawnCfg)
	if err != nil {
		if dispositionErr := l.store.RecordDisposition(
			ctx, runID, plan.Disposition(0), nil, l.clock.Now(), nil,
		); dispositionErr != nil {
			return dispositionErr
		}
		return l.event(ctx, ticket.URL, eventSpawnFailed, err.Error())
	}

	if err := l.store.RecordSpawn(ctx, runID, result.Pid, l.clock.Now(), logPath); err != nil {
		return err
	}
	l.spawned = append(l.spawned, ticket.URL)
	return l.event(ctx, ticket.URL, eventRunLaunched, fmt.Sprintf("spawned pid %d in %s", result.Pid, spec.worktree))
}
