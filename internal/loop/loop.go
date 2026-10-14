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
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// Loop is the reconcile loop: observe, decide, act. It is the only writer of reconciled state.
type Loop struct {
	logger     *slog.Logger
	store      *store.Store
	observe    ObserveFunc
	clock      Clock
	forge      forge
	worktrees  worktrees
	validate   ValidateFunc
	remoteOf   RemoteFunc
	runner     agentRunner
	cfg        config.Config
	ws         config.Workspace
	trackerFor tracker.Resolver
	nudgeCh    chan struct{}
}

func (l *Loop) checkout(repo string) string { return config.CheckoutPath(l.cfg.DataDir, repo) }

func (l *Loop) event(ctx context.Context, ticketURL string, kind store.EventKind, detail string) error {
	ev := store.Event{At: l.clock.Now(), TicketURL: ticketURL, Kind: kind, Detail: detail}
	if err := l.store.AppendEvent(ctx, ev); err != nil {
		return fmt.Errorf("append %s event for %s: %w", kind, ticketURL, err)
	}
	return nil
}

var errIntentPending = errors.New("intent stays pending")

func (l *Loop) eachIntent(ctx context.Context, verb plan.Verb, fn func(store.VerbIntent) error) error {
	intents, err := l.store.PendingVerbIntents(ctx, verb)
	if err != nil {
		return fmt.Errorf("list pending %s intents: %w", verb, err)
	}
	for _, intent := range intents {
		if err := fn(intent); errors.Is(err, errIntentPending) {
			continue
		} else if err != nil {
			return fmt.Errorf("intent %d for %s: %w", intent.ID, intent.TicketID, err)
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, l.clock.Now()); err != nil {
			return fmt.Errorf("consume intent %d for %s: %w", intent.ID, intent.TicketID, err)
		}
	}
	return nil
}

func (l *Loop) ticket(ctx context.Context, url string) (store.Ticket, bool, error) {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return store.Ticket{}, false, fmt.Errorf("list tickets: %w", err)
	}
	i := slices.IndexFunc(tickets, func(t store.Ticket) bool { return t.URL == url })
	if i < 0 {
		return store.Ticket{}, false, nil
	}
	return tickets[i], true, nil
}

// RunOnce runs one tick. A failed observe records the error and leaves the last good
// observation in place rather than applying any transition, so the page's observe age
// keeps growing instead of resetting.
func (l *Loop) RunOnce(ctx context.Context) error {
	if err := l.applyTrackIntents(ctx); err != nil {
		return l.failTick(ctx, fmt.Errorf("track: %w", err))
	}
	if err := l.validateCloningRepos(ctx); err != nil {
		return l.failTick(ctx, fmt.Errorf("validate repos: %w", err))
	}
	if err := l.applyImportIntents(ctx); err != nil {
		return l.failTick(ctx, fmt.Errorf("import: %w", err))
	}
	if err := l.applyEditTicketIntents(ctx); err != nil {
		return l.failTick(ctx, fmt.Errorf("edit ticket: %w", err))
	}

	obs, err := l.observe(ctx)
	if err != nil {
		at := l.clock.Now()
		tickErr := fmt.Errorf("observe: %w", err)
		if recordErr := l.store.RecordTickError(ctx, store.TickError{At: at, Message: tickErr.Error()}); recordErr != nil {
			return fmt.Errorf("record observe failure: %w", errors.Join(tickErr, recordErr))
		}
		return tickErr
	}

	t := &tick{now: l.clock.Now(), obs: obs}
	t.obs.ObservedAt = t.now
	if t.obs.LocalTips == nil {
		t.obs.LocalTips = map[string]string{}
	}
	if t.obs.Runs == nil {
		t.obs.Runs = map[string]plan.RunObservation{}
	}
	if err := l.absorb(ctx, t); err != nil {
		return l.failTick(ctx, err)
	}
	if err := l.recordRepoProblems(ctx, t.obs); err != nil {
		return l.failTick(ctx, fmt.Errorf("record repo problems: %w", err))
	}
	skipped, err := l.skippedRepos(ctx, t.obs)
	if err != nil {
		return l.failTick(ctx, fmt.Errorf("skipped repos: %w", err))
	}
	snap, err := l.derive(ctx, t.obs)
	if err != nil {
		return l.failTick(ctx, fmt.Errorf("derive: %w", err))
	}
	return l.failTick(ctx, l.act(ctx, snap.Skipping(skipped), t))
}

func (l *Loop) failTick(ctx context.Context, err error) error {
	if err == nil || ctx.Err() != nil {
		return err
	}
	if recordErr := l.recordTickError(ctx, err.Error()); recordErr != nil {
		return fmt.Errorf("record tick error: %w", errors.Join(err, recordErr))
	}
	return err
}

func (l *Loop) absorb(ctx context.Context, t *tick) error {
	prev, _, err := l.store.LastObservation(ctx)
	if err != nil {
		return fmt.Errorf("last observation: %w", err)
	}
	if err := l.recordRepoStates(ctx, t.obs); err != nil {
		return fmt.Errorf("record repo states: %w", err)
	}
	if err := l.tickCheckingWaits(ctx); err != nil {
		return fmt.Errorf("tick checking waits: %w", err)
	}
	if err := l.recordOpenedEvents(ctx, prev, t.obs); err != nil {
		return fmt.Errorf("opened events: %w", err)
	}
	if err := l.recordChecksLanded(ctx, prev, t.obs); err != nil {
		return fmt.Errorf("checks landed: %w", err)
	}
	if err := l.store.SaveObservation(ctx, t.obs); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}
	if err := l.store.ApplyLaunchIntents(ctx, l.clock.Now()); err != nil {
		return fmt.Errorf("apply launch intents: %w", err)
	}
	if err := l.applyCancelIntents(ctx); err != nil {
		return fmt.Errorf("cancel: %w", err)
	}
	if err := l.applyKillIntents(ctx); err != nil {
		return fmt.Errorf("kill: %w", err)
	}
	if err := l.interruptForFollowUps(ctx); err != nil {
		return fmt.Errorf("interrupt for follow-ups: %w", err)
	}
	if err := l.reconcileRuns(ctx, t.obs); err != nil {
		return fmt.Errorf("reconcile runs: %w", err)
	}
	if err := l.store.SaveObservation(ctx, t.obs); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}
	if err := l.recordVerdictTransitions(ctx, t.obs); err != nil {
		return fmt.Errorf("verdict transitions: %w", err)
	}
	if err := l.recordMergedEvents(ctx, t.obs); err != nil {
		return fmt.Errorf("merged events: %w", err)
	}
	return nil
}

func (l *Loop) act(ctx context.Context, snap plan.Snapshot, t *tick) error {
	if err := l.applyReRunIntents(ctx, snap, t); err != nil {
		return fmt.Errorf("re-run: %w", err)
	}
	if err := l.applyFollowUpIntents(ctx, t); err != nil {
		return fmt.Errorf("follow-up: %w", err)
	}
	if err := l.applyAbortIntents(ctx, snap, t.obs); err != nil {
		return fmt.Errorf("abort: %w", err)
	}
	t.markSpawnedAlive()
	if err := l.retargetMerged(ctx, snap, t.obs); err != nil {
		return fmt.Errorf("retarget: %w", err)
	}
	if err := l.applyRefreshIntents(ctx, snap, t.obs); err != nil {
		return fmt.Errorf("refresh: %w", err)
	}
	l.rereadLocalTips(ctx, t.obs)
	if err := l.applyRetryPushIntents(ctx, snap, t.obs); err != nil {
		return fmt.Errorf("retry push: %w", err)
	}
	if err := l.applyRemoveWorktreeIntents(ctx, snap, t.obs); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	if err := l.pushPushable(ctx, snap, t.obs); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	if err := l.applyDraftGate(ctx, snap); err != nil {
		return fmt.Errorf("draft gate: %w", err)
	}
	if err := l.launchEligible(ctx, snap, t); err != nil {
		return fmt.Errorf("launch: %w", err)
	}
	return nil
}

func (t *tick) markSpawnedAlive() {
	for _, url := range t.spawned {
		t.obs.Runs[url] = plan.RunObservation{Alive: true}
	}
}

func (l *Loop) derive(ctx context.Context, obs plan.Observation) (plan.Snapshot, error) {
	in, err := l.store.PlanInput(ctx)
	if err != nil {
		return plan.Snapshot{}, fmt.Errorf("load plan input: %w", err)
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
		return fmt.Errorf("list repos: %w", err)
	}
	for _, repo := range repos {
		current := plan.RepoStatus{
			Name: repo.Name, Phase: repo.Phase, RefusalKind: repo.RefusalKind, Refusal: repo.Refusal,
		}
		next := plan.NextRepoStatus(current, obs)
		source, read := obs.SettingsSources[repo.Name]
		if next == current && (!read || source == repo.SettingsSource) {
			continue
		}
		repo.Phase, repo.RefusalKind, repo.Refusal = next.Phase, next.RefusalKind, next.Refusal
		if read {
			repo.SettingsSource, repo.SettingsReadAt = source, l.clock.Now()
		}
		if err := l.store.SetRepoState(ctx, repo); err != nil {
			return fmt.Errorf("set state of %s: %w", repo.Name, err)
		}
	}
	return nil
}

// recordRepoProblems puts every repo the loop cannot drive this tick on the masthead's last
// error: unreadable settings, a failed observe, or a refusal that waits for Track.
func (l *Loop) recordRepoProblems(ctx context.Context, obs plan.Observation) error {
	repos, err := l.store.Repos(ctx)
	if err != nil {
		return fmt.Errorf("list repos: %w", err)
	}
	var parts []string
	for repo, reason := range obs.SettingsErrors {
		parts = append(parts, fmt.Sprintf("settings for %s: %s", repo, reason))
	}
	for repo, reason := range obs.RepoErrors {
		parts = append(parts, fmt.Sprintf("observe %s: %s", repo, reason))
	}
	for _, repo := range repos {
		if repo.Phase == plan.RepoRefused && repo.RefusalKind != plan.RefusalSettingsParse {
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
		return nil, fmt.Errorf("list repos: %w", err)
	}
	skipped := map[string]bool{}
	for _, repo := range repos {
		if repo.Phase != plan.RepoReady {
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
	return l.eachIntent(ctx, plan.VerbImport, func(intent store.VerbIntent) error {
		return l.importFeature(ctx, intent.TicketID)
	})
}

func (l *Loop) importFeature(ctx context.Context, feature string) error {
	lastObs, _, err := l.store.LastObservation(ctx)
	if err != nil {
		return fmt.Errorf("last observation: %w", err)
	}
	repos, err := ReadyRepos(ctx, l.store)
	if err != nil {
		return fmt.Errorf("list ready repos: %w", err)
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
	var closure *store.FeatureClosureError
	switch {
	case errors.As(err, &conflict):
		err = l.store.RecordImportRefusal(ctx, feature, conflict, l.clock.Now())
	case errors.As(err, &closure):
		err = l.store.RecordImportRefusal(ctx, feature, closure, l.clock.Now())
	}
	if err != nil {
		return fmt.Errorf("import tickets for %s: %w", feature, err)
	}
	return nil
}

func (l *Loop) applyEditTicketIntents(ctx context.Context) error {
	intents, err := l.store.PendingEditTicketIntents(ctx)
	if err != nil {
		return fmt.Errorf("list pending edit-ticket intents: %w", err)
	}

	now := l.clock.Now()
	for _, intent := range intents {
		err := l.store.EditTicket(ctx, intent.TicketID, intent.Branch, intent.BlockedBy)
		var closure *store.FeatureClosureError
		if errors.As(err, &closure) {
			if err := l.store.RecordImportRefusal(ctx, closure.Feature, closure, now); err != nil {
				return fmt.Errorf("record import refusal for %s: %w", intent.TicketID, err)
			}
		} else if err != nil {
			return fmt.Errorf("edit %s: %w", intent.TicketID, err)
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return fmt.Errorf("consume intent %d for %s: %w", intent.ID, intent.TicketID, err)
		}
	}
	return nil
}

func (l *Loop) applyKillIntents(ctx context.Context) error {
	return l.eachIntent(ctx, plan.VerbKill, func(intent store.VerbIntent) error {
		latest, err := l.store.LatestRunsByTicket(ctx)
		if err != nil {
			return fmt.Errorf("latest runs: %w", err)
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
	if err != nil {
		return fmt.Errorf("list pending follow-up intents: %w", err)
	}
	if len(intents) == 0 {
		return nil
	}
	latest, err := l.store.LatestRunsByTicket(ctx)
	if err != nil {
		return fmt.Errorf("latest runs: %w", err)
	}
	cancelled := map[string]bool{}
	for _, intent := range intents {
		run, ok := latest[intent.TicketID]
		steerable := run.Kind == plan.RunKindAgent || run.Kind == plan.RunKindFollowUp
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
		return fmt.Errorf("list runs awaiting disposition: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return fmt.Errorf("list tickets: %w", err)
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
			return fmt.Errorf("dispose run %d of %s: %w", run.ID, run.TicketID, err)
		}
	}
	return nil
}

func (l *Loop) disposeRun(
	ctx context.Context, run store.PendingRun, ticket store.Ticket, obs plan.Observation,
) error {
	outcome, err := l.outcomeFor(ctx, run, ticket, obs)
	if err != nil {
		return fmt.Errorf("outcome: %w", err)
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
	if err := l.event(ctx, ticket.URL, store.EventRunDisposed, outcome.String()); err != nil {
		return fmt.Errorf("append run disposed event: %w", err)
	}
	return nil
}

func (l *Loop) outcomeFor(
	ctx context.Context, run store.PendingRun, ticket store.Ticket, obs plan.Observation,
) (plan.Outcome, error) {
	if run.Kind == plan.RunKindAgent || run.Kind == plan.RunKindFollowUp {
		queued, err := l.followUpQueued(ctx, run.TicketID)
		if err != nil {
			return 0, fmt.Errorf("check queued follow-up for %s: %w", run.TicketID, err)
		}
		if queued {
			return plan.OutcomeContinued, nil
		}
	}
	question, err := l.readQuestion(run)
	if err != nil {
		return 0, fmt.Errorf("read question for run %d: %w", run.ID, err)
	}
	if question != "" {
		if err := l.store.RecordQuestion(ctx, run.ID, question); err != nil {
			return 0, fmt.Errorf("record question for run %d: %w", run.ID, err)
		}
		return plan.OutcomeAsked, nil
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
		return false, fmt.Errorf("list pending follow-up intents: %w", err)
	}
	return slices.ContainsFunc(intents, func(i store.VerbIntent) bool { return i.TicketID == ticketURL }), nil
}

const maxQuestionBytes = 4096

func (l *Loop) questionPath(runID int64) string {
	return filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.question", runID))
}

func (l *Loop) readQuestion(run store.PendingRun) (string, error) {
	if run.Kind != plan.RunKindAgent && run.Kind != plan.RunKindFollowUp {
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
		commits, err := git.CommitsSince(ctx, worktreePath, baselineSHA, "HEAD")
		if err != nil {
			return 0, fmt.Errorf("count commits in %s: %w", worktreePath, err)
		}
		return commits, nil
	}
	tip, ok := obs.BranchTips[plan.BranchKey(ticket.Repo, ticket.Branch)]
	if !ok {
		return 0, nil
	}
	repoPath := l.checkout(ticket.Repo)
	commits, err := git.CommitsSince(ctx, repoPath, baselineSHA, tip)
	if err != nil {
		return 0, fmt.Errorf("count commits on %s/%s: %w", ticket.Repo, ticket.Branch, err)
	}
	return commits, nil
}

func (l *Loop) launchEligible(ctx context.Context, snap plan.Snapshot, t *tick) error {
	toLaunch := snap.LaunchAfter(len(t.spawned))
	if len(toLaunch) == 0 {
		return nil
	}

	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return fmt.Errorf("list tickets: %w", err)
	}
	byTicket := ticketsByURL(tickets)
	for _, ticketURL := range toLaunch {
		entry, _ := snap.Entry(ticketURL)
		ticket := byTicket[ticketURL]
		if err := l.cutAndSpawn(ctx, t, ticket, entry.Unlock.BaseBranch, entry.PromptHash); err != nil {
			return fmt.Errorf("%s: %w", ticketURL, err)
		}
	}
	return nil
}

func (l *Loop) tickCheckingWaits(ctx context.Context) error {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return fmt.Errorf("list tickets: %w", err)
	}
	urls := make([]string, len(tickets))
	for i, t := range tickets {
		urls[i] = t.URL
	}
	if err := l.store.IncrementCheckingTicks(ctx, urls); err != nil {
		return fmt.Errorf("increment checking ticks: %w", err)
	}
	return nil
}

func ticketsByURL(tickets []store.Ticket) map[string]store.Ticket {
	byURL := make(map[string]store.Ticket, len(tickets))
	for _, t := range tickets {
		byURL[t.URL] = t
	}
	return byURL
}

func (l *Loop) cutAndSpawn(
	ctx context.Context, t *tick, ticket store.Ticket, baseBranch, promptHash string,
) error {
	branch := ticket.Branch
	repoPath := l.checkout(ticket.Repo)

	if err := l.worktrees.New(ctx, repoPath, branch, "origin/"+baseBranch); err != nil {
		if _, insertErr := l.store.InsertCutFailedRun(ctx, ticket.URL, promptHash, l.clock.Now()); insertErr != nil {
			return errors.Join(
				fmt.Errorf("cut %s: %w", branch, err),
				fmt.Errorf("record failed cut of %s: %w", branch, insertErr),
			)
		}
		return l.event(ctx, ticket.URL, store.EventCutFailed, err.Error())
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

	if err := l.spawnRun(ctx, t, spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: baselineSHA, hash: promptHash,
		kind: plan.RunKindAgent, prompt: agentPrompt(ticket),
	}); err != nil {
		return fmt.Errorf("spawn agent in %s: %w", worktreePath, err)
	}
	return nil
}

func agentPrompt(ticket store.Ticket) string {
	prompt := plan.Compose(ticket.Plan())
	if ticket.Body != "" {
		prompt += "\n\n## Ticket\n\n" + ticket.Body
	}
	return prompt
}

type spawnSpec struct {
	ticket                           store.Ticket
	worktree, baseline, hash, prompt string
	kind                             plan.RunKind
	promptFor                        func(runID int64) string
	resumeSessionID, message         string
}

func (spec spawnSpec) session() store.RunSession {
	switch {
	case spec.resumeSessionID != "":
		return store.RunSession{JoinID: spec.resumeSessionID, Message: spec.message}
	case spec.kind == plan.RunKindAgent || spec.kind == plan.RunKindFollowUp:
		return store.RunSession{StartNew: true, Message: spec.message}
	default:
		return store.RunSession{}
	}
}

func (l *Loop) insertSkeleton(ctx context.Context, spec spawnSpec) (runID int64, sessionID string, err error) {
	return l.store.InsertSessionRunSkeleton(
		ctx, spec.ticket.URL, spec.kind, spec.baseline, spec.hash, spec.session())
}

func (l *Loop) spawnRun(ctx context.Context, t *tick, spec spawnSpec) (err error) {
	ticket := spec.ticket
	runID, sessionID, err := l.insertSkeleton(ctx, spec)
	if err != nil {
		return fmt.Errorf("insert run skeleton for %s: %w", ticket.URL, err)
	}

	promptFor := spec.promptFor
	if promptFor == nil && (spec.kind == plan.RunKindAgent || spec.kind == plan.RunKindFollowUp) {
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
		Model:        config.AgentModel,
		Prompt:       prompt,
		PromptPath:   promptPath,
		SessionID:    sessionID,
		Resume:       spec.resumeSessionID != "",
		LogFile:      logFile,
	}
	if spec.kind == plan.RunKindAgent {
		spawnCfg.SystemPromptPath = l.ws.SystemPromptPath
	}
	result, err := l.runner.Spawn(ctx, spawnCfg)
	if err != nil {
		if dispositionErr := l.store.RecordDisposition(
			ctx, runID, plan.Disposition(0), nil, l.clock.Now(), nil,
		); dispositionErr != nil {
			return errors.Join(
				fmt.Errorf("spawn run %d: %w", runID, err),
				fmt.Errorf("record disposition for failed spawn of run %d: %w", runID, dispositionErr),
			)
		}
		return l.event(ctx, ticket.URL, store.EventSpawnFailed, err.Error())
	}

	if err := l.store.RecordSpawn(ctx, runID, result.Pid, l.clock.Now(), logPath); err != nil {
		return fmt.Errorf("record spawn of run %d: %w", runID, err)
	}
	t.spawned = append(t.spawned, ticket.URL)
	return l.event(ctx, ticket.URL, store.EventRunLaunched, fmt.Sprintf("spawned pid %d in %s", result.Pid, spec.worktree))
}
