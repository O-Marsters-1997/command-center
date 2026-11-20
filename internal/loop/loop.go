package loop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/treepad"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type Loop struct {
	logger     *slog.Logger
	store      *store.Store
	observe    ObserveFunc
	clock      Clock
	forge      forge
	worktrees  worktrees
	notifier   notifier
	validate   ValidateFunc
	remoteOf   RemoteFunc
	runner     runner.Runner
	cfg        config.Config
	ws         config.Workspace
	trackerFor tracker.Resolver
	nudgeCh    chan struct{}
	workflows  map[string]plan.Workflow
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

func (l *Loop) eachIntent(ctx context.Context, snap plan.Snapshot, verb plan.Verb, fn func(plan.Intent) error) error {
	return l.consumeEach(ctx, snap.Intents(verb), fn)
}

func (l *Loop) eachPendingIntent(ctx context.Context, verb plan.Verb, fn func(plan.Intent) error) error {
	pending, err := l.store.PendingVerbIntents(ctx, verb)
	if err != nil {
		return fmt.Errorf("list pending %s intents: %w", verb, err)
	}
	intents := make([]plan.Intent, len(pending))
	for i, p := range pending {
		intents[i] = plan.Intent{
			ID: p.ID, Ticket: plan.TicketURL(p.TicketID), Verb: verb, Payload: p.Payload, HoldUntil: p.HoldUntil,
		}
	}
	return l.consumeEach(ctx, intents, fn)
}

func (l *Loop) consumeEach(ctx context.Context, intents []plan.Intent, fn func(plan.Intent) error) error {
	now := l.clock.Now()
	for _, intent := range intents {
		if intent.HeldAt(now) {
			continue
		}
		switch err := fn(intent); {
		case errors.Is(err, errIntentPending):
			continue
		case err != nil:
			return fmt.Errorf("intent %d for %s: %w", intent.ID, intent.Ticket, err)
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, l.clock.Now()); err != nil {
			return fmt.Errorf("consume intent %d for %s: %w", intent.ID, intent.Ticket, err)
		}
	}
	return nil
}

func (l *Loop) ticket(byURL map[plan.TicketURL]store.Ticket, url string) (store.Ticket, bool) {
	ticket, ok := byURL[plan.TicketURL(url)]
	return ticket, ok
}

func (l *Loop) RunOnce(ctx context.Context) error {
	var timing tickTiming
	defer func() { timing.log(ctx, l.logger) }()
	if err := l.store.PruneHistory(ctx, l.clock.Now()); err != nil {
		l.logger.Warn("prune history", "err", err)
	}
	if err := l.applyTrackIntents(ctx); err != nil {
		return l.failTick(ctx, fmt.Errorf("track: %w", err))
	}
	if err := l.timed(&timing.Validate, func() error { return l.validateCloningRepos(ctx) }); err != nil {
		return l.failTick(ctx, fmt.Errorf("validate repos: %w", err))
	}
	if err := l.applyImportIntents(ctx); err != nil {
		return l.failTick(ctx, fmt.Errorf("import: %w", err))
	}
	if err := l.applyEditTicketIntents(ctx); err != nil {
		return l.failTick(ctx, fmt.Errorf("edit ticket: %w", err))
	}

	var obs plan.Observation
	err := l.timed(&timing.Observe, func() error {
		var err error
		if obs, err = l.observe(ctx); err != nil {
			return err
		}
		return l.observeRuns(ctx, &obs)
	})
	if err != nil {
		at := l.clock.Now()
		tickErr := fmt.Errorf("observe: %w", err)
		if recordErr := l.store.RecordTickError(ctx, store.TickError{At: at, Message: tickErr.Error()}); recordErr != nil {
			return fmt.Errorf("record observe failure: %w", errors.Join(tickErr, recordErr))
		}
		return tickErr
	}

	timing.Repos = observedRepos(obs)
	for _, run := range obs.Runs {
		if run.Alive {
			timing.LiveCommandRuns++
		}
	}

	t := &tick{now: l.clock.Now(), obs: obs, cancelled: map[int]bool{}, timing: &timing}
	t.obs.ObservedAt = t.now
	var byURL map[plan.TicketURL]store.Ticket
	err = l.timed(&timing.Absorb, func() error {
		var err error
		byURL, err = l.absorb(ctx, t)
		return err
	})
	if err != nil {
		return l.failTick(ctx, err)
	}
	snap, err := l.derive(ctx, t)
	if err != nil {
		return l.failTick(ctx, fmt.Errorf("derive: %w", err))
	}
	if err := l.recordRepoProblems(ctx, snap); err != nil {
		return l.failTick(ctx, fmt.Errorf("record repo problems: %w", err))
	}
	return l.failTick(ctx, l.act(ctx, snap.Skipping(snap.SkippedRepos()), t, byURL))
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

func (l *Loop) absorb(ctx context.Context, t *tick) (map[plan.TicketURL]store.Ticket, error) {
	tickets, err := l.store.Tickets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tickets: %w", err)
	}
	byURL := ticketsByURL(tickets)
	prev, _, err := l.store.LastObservation(ctx)
	if err != nil {
		return nil, fmt.Errorf("last observation: %w", err)
	}
	t.prev = prev
	if err := l.recordRepoStates(ctx, t.obs); err != nil {
		return nil, fmt.Errorf("record repo states: %w", err)
	}
	if err := l.tickCheckingWaits(ctx, tickets); err != nil {
		return nil, fmt.Errorf("tick checking waits: %w", err)
	}
	if err := l.store.ApplyLaunchIntents(ctx, l.clock.Now()); err != nil {
		return nil, fmt.Errorf("apply launch intents: %w", err)
	}
	if err := l.applyCancelIntents(ctx); err != nil {
		return nil, fmt.Errorf("cancel: %w", err)
	}
	if err := l.applyKillIntents(ctx, t); err != nil {
		return nil, fmt.Errorf("kill: %w", err)
	}
	if err := l.killOverdue(ctx, t); err != nil {
		return nil, fmt.Errorf("kill overdue: %w", err)
	}
	if err := l.interruptForFollowUps(ctx, t); err != nil {
		return nil, fmt.Errorf("interrupt for follow-ups: %w", err)
	}
	if err := l.reconcileRuns(ctx, t, byURL); err != nil {
		return nil, fmt.Errorf("reconcile runs: %w", err)
	}
	if err := l.store.SaveObservation(ctx, t.obs); err != nil {
		return nil, fmt.Errorf("save observation: %w", err)
	}
	return byURL, nil
}

func (l *Loop) act(ctx context.Context, snap plan.Snapshot, t *tick, byURL map[plan.TicketURL]store.Ticket) error {
	if err := l.recordTransitions(ctx, snap); err != nil {
		return fmt.Errorf("transitions: %w", err)
	}
	l.rerunFailedChecks(ctx, snap, byURL)
	if err := l.applyReRunIntents(ctx, snap, t, byURL); err != nil {
		return fmt.Errorf("re-run: %w", err)
	}
	if err := l.applyFollowUpIntents(ctx, snap, t, byURL); err != nil {
		return fmt.Errorf("follow-up: %w", err)
	}
	if err := l.applyAutoFollowUps(ctx, snap, t, byURL); err != nil {
		return fmt.Errorf("auto follow-up: %w", err)
	}
	if err := l.applyResolveIntents(ctx, snap, t, byURL); err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	if err := l.applyAbortIntents(ctx, snap, t.obs); err != nil {
		return fmt.Errorf("abort: %w", err)
	}
	t.markSpawnedAlive()
	if err := l.retargetMerged(ctx, snap, t.obs); err != nil {
		return fmt.Errorf("retarget: %w", err)
	}
	if err := l.timed(&t.timing.Refresh, func() error { return l.applyRefreshIntents(ctx, snap, t.obs) }); err != nil {
		return fmt.Errorf("refresh: %w", err)
	}
	l.rereadLocalTips(ctx, t.obs)
	if err := l.applyRetryPushIntents(ctx, snap, t.obs); err != nil {
		return fmt.Errorf("retry push: %w", err)
	}
	if err := l.applyRaiseIntents(ctx, snap, t.obs); err != nil {
		return fmt.Errorf("raise: %w", err)
	}
	removeWorktrees := func() error { return l.applyRemoveWorktreeIntents(ctx, snap, t.obs) }
	if err := l.timed(&t.timing.Removal, removeWorktrees); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	if err := l.queueAutoRemovals(ctx, snap); err != nil {
		return fmt.Errorf("auto removal: %w", err)
	}
	snap, err := l.derive(ctx, t)
	if err != nil {
		return fmt.Errorf("derive for push: %w", err)
	}
	pushable := snap.Skipping(snap.SkippedRepos())
	if err := l.timed(&t.timing.Push, func() error { return l.pushPushable(ctx, pushable, t.obs) }); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	if err := l.applyDraftGate(ctx, snap); err != nil {
		return fmt.Errorf("draft gate: %w", err)
	}
	if err := l.timed(&t.timing.Cut, func() error { return l.launchEligible(ctx, snap, t, byURL) }); err != nil {
		return fmt.Errorf("launch: %w", err)
	}
	if err := l.spawnVerifies(ctx, pushable, t, byURL); err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	return nil
}

func (t *tick) markSpawnedAlive() {
	for _, url := range t.spawned {
		t.obs.MarkAlive(url)
	}
}

func (l *Loop) derive(ctx context.Context, t *tick) (plan.Snapshot, error) {
	in, err := l.store.PlanInput(ctx)
	if err != nil {
		return plan.Snapshot{}, fmt.Errorf("load plan input: %w", err)
	}
	in.Now = l.clock.Now()
	in.Intents = releasedIntents(in.Intents, in.Now)
	in.Obs = t.obs
	in.Prev = t.prev
	return l.rules(t.obs).Derive(in), nil
}

func releasedIntents(byTicket map[plan.TicketURL][]plan.Intent, now time.Time) map[plan.TicketURL][]plan.Intent {
	released := make(map[plan.TicketURL][]plan.Intent, len(byTicket))
	for url, intents := range byTicket {
		released[url] = slices.DeleteFunc(slices.Clone(intents), func(i plan.Intent) bool { return i.HeldAt(now) })
	}
	return released
}

func (l *Loop) rules(obs plan.Observation) plan.Rules {
	daemon := plan.Daemon{
		MaxAgents:    l.cfg.MaxAgents,
		MaxVerifies:  l.cfg.MaxVerifies,
		SpendLimit5h: l.cfg.SpendLimit5h,
		Workflows:    l.workflows,
	}
	return plan.RulesFor(daemon, obs)
}

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

func (l *Loop) recordRepoProblems(ctx context.Context, snap plan.Snapshot) error {
	parts := snap.RepoProblems()
	if len(parts) == 0 {
		return nil
	}
	return l.recordTickError(ctx, strings.Join(parts, "; "))
}

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
	return l.eachPendingIntent(ctx, plan.VerbImport, func(intent plan.Intent) error {
		return l.importFeature(ctx, string(intent.Ticket), intent.Payload)
	})
}

func (l *Loop) importFeature(ctx context.Context, feature, onlyRepo string) error {
	lastObs, _, err := l.store.LastObservation(ctx)
	if err != nil {
		return fmt.Errorf("last observation: %w", err)
	}
	repos, err := readyRepos(ctx, l.store)
	if err != nil {
		return fmt.Errorf("list ready repos: %w", err)
	}
	var matched []store.ImportedTicket
	for _, repo := range repos {
		if onlyRepo != "" && repo.Name != onlyRepo {
			continue
		}
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

	err = l.store.ImportRepoTickets(ctx, feature, onlyRepo, matched, l.clock.Now())
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
		switch {
		case errors.As(err, &closure):
			if err := l.store.RecordImportRefusal(ctx, closure.Feature, closure, now); err != nil {
				return fmt.Errorf("record import refusal for %s: %w", intent.TicketID, err)
			}
		case err != nil:
			return fmt.Errorf("edit %s: %w", intent.TicketID, err)
		}
		if err := l.store.ConsumeVerbIntent(ctx, intent.ID, now); err != nil {
			return fmt.Errorf("consume intent %d for %s: %w", intent.ID, intent.TicketID, err)
		}
	}
	return nil
}

func (l *Loop) cancelRun(t *tick, ticketURL string, pgid int) error {
	if err := l.runner.Cancel(pgid); err != nil {
		return fmt.Errorf("cancel %s (pgid %d): %w", ticketURL, pgid, err)
	}
	t.cancelled[pgid] = true
	return nil
}

func (l *Loop) applyKillIntents(ctx context.Context, t *tick) error {
	return l.eachPendingIntent(ctx, plan.VerbKill, func(intent plan.Intent) error {
		latest, err := l.store.LatestRunsByTicket(ctx)
		if err != nil {
			return fmt.Errorf("latest runs: %w", err)
		}
		if run, ok := latest[intent.Ticket]; ok && run.Pgid.IsSome() && run.Outcome.IsNone() {
			return l.cancelRun(t, string(intent.Ticket), run.Pgid.Unwrap())
		}
		return nil
	})
}

func (l *Loop) killOverdue(ctx context.Context, t *tick) error {
	snap, err := l.derive(ctx, t)
	if err != nil {
		return fmt.Errorf("derive: %w", err)
	}
	overdue := snap.Overdue()
	if len(overdue) == 0 {
		return nil
	}
	pending, err := l.store.PendingRunsAwaitingDisposition(ctx)
	if err != nil {
		return fmt.Errorf("list runs awaiting disposition: %w", err)
	}
	for _, run := range pending {
		if slices.Contains(overdue, plan.RunID(run.ID)) && !t.cancelled[run.Pgid] {
			l.logger.WarnContext(ctx, "killing command run past its deadline", "run", run.ID, "ticket", run.TicketID)
			if err := l.cancelRun(t, run.TicketID, run.Pgid); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *Loop) interruptForFollowUps(ctx context.Context, t *tick) error {
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
	cancelled := map[plan.TicketURL]bool{}
	now := l.clock.Now()
	for _, intent := range intents {
		if intent.HoldUntil.After(now) {
			continue
		}
		url := plan.TicketURL(intent.TicketID)
		run, ok := latest[url]
		steerable := run.Kind == plan.RunKindAgent || run.Kind == plan.RunKindFollowUp
		if cancelled[url] || !ok || !steerable || run.Pgid.IsNone() || run.Outcome.IsSome() {
			continue
		}
		cancelled[url] = true
		if err := l.cancelRun(t, intent.TicketID, run.Pgid.Unwrap()); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) reconcileRuns(ctx context.Context, t *tick, byTicket map[plan.TicketURL]store.Ticket) error {
	pending, err := l.store.PendingRunsAwaitingDisposition(ctx)
	if err != nil {
		return fmt.Errorf("list runs awaiting disposition: %w", err)
	}
	if len(pending) == 0 {
		return nil
	}

	for _, run := range pending {
		url := plan.TicketURL(run.TicketID)
		observed, seen := t.obs.Runs[url]
		if !seen || observed.Alive {
			if seen && !t.cancelled[run.Pgid] {
				continue
			}
			alive, err := l.runner.Liveness(run.Pgid, run.ProcStartedAt, l.clock.Now())
			if err != nil {
				return fmt.Errorf("liveness for run %d: %w", run.ID, err)
			}
			if alive {
				t.obs.MarkAlive(url)
				continue
			}
			t.obs.Runs[url] = plan.RunObservation{}
			question, wrote, err := l.readQuestionFile(plan.RunID(run.ID))
			if err != nil {
				return err
			}
			if wrote {
				t.obs.Questions[plan.RunID(run.ID)] = question
			}
		}
		if err := l.disposeRun(ctx, run, byTicket[url], t.obs); err != nil {
			return fmt.Errorf("dispose run %d of %s: %w", run.ID, run.TicketID, err)
		}
	}
	return nil
}

func (l *Loop) disposeRun(
	ctx context.Context, run store.PendingRun, ticket store.Ticket, obs plan.Observation,
) error {
	exitCode := option.None[int]()
	if code, ok := l.runner.Reap(run.Pgid); ok {
		exitCode = option.Some(code)
	}
	lastMessage, _ := parseLog(l.logger, run.LogPath, "last message", agentlog.ParseLastMessage)
	outcome, err := l.outcomeFor(ctx, run, ticket, obs, exitCode, lastMessage)
	if err != nil {
		return fmt.Errorf("outcome: %w", err)
	}
	if _, step, known := l.stepOf(run); known {
		l.writeHandoff(run, step, lastMessage, outcome == plan.OutcomeFailed)
	}
	cost := option.None[float64]()
	metrics := option.None[agentlog.RunMetrics]()
	if m, ok := parseLog(l.logger, run.LogPath, "run metrics", agentlog.ParseMetrics); ok {
		cost = m.CostUSD
		metrics = option.Some(m)
	}
	disposition := store.Disposition{
		RunID: plan.RunID(run.ID), Outcome: outcome, ExitCode: exitCode, Cost: cost,
		LastMessage: lastMessage, Metrics: metrics, EndedAt: l.clock.Now(),
	}
	if err := l.store.RecordDisposition(ctx, disposition); err != nil {
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
	exitCode option.Option[int], lastMessage option.Option[string],
) (plan.Outcome, error) {
	if run.Kind == plan.RunKindVerify {
		if exitCode == option.Some(0) {
			return plan.OutcomePassed, nil
		}
		return plan.OutcomeFailed, nil
	}
	if run.Kind == plan.RunKindAgent || run.Kind == plan.RunKindFollowUp {
		queued, err := l.followUpQueued(ctx, run.TicketID)
		if err != nil {
			return 0, fmt.Errorf("check queued follow-up for %s: %w", run.TicketID, err)
		}
		if queued {
			return plan.OutcomeContinued, nil
		}
	}
	questionFile := option.None[string]()
	if raw, ok := obs.Questions[plan.RunID(run.ID)]; ok {
		questionFile = option.Some(raw)
	}
	if question := plan.Asked(run.Kind, questionFile); question.IsSome() {
		if err := l.store.RecordQuestion(ctx, run.ID, question.Unwrap()); err != nil {
			return 0, fmt.Errorf("record question for run %d: %w", run.ID, err)
		}
		return plan.OutcomeAsked, nil
	}
	baseline := run.BaselineSHA
	w, step, known := l.stepOf(run)
	if known && len(w.Steps) > 1 {
		hasVerify := len(obs.Settings[ticket.Repo].VerifyCommand) > 0
		if !finalStep(w, step) {
			evidence := plan.StepEvidence{ExitCode: exitCode, Handoff: lastMessage}
			if !step.Command && !step.Writes && baseline != "" {
				var err error
				evidence.Commits, err = l.commitsSinceBaseline(ctx, ticket, obs, plan.SHA(baseline))
				if err != nil {
					return 0, fmt.Errorf("commits since baseline for run %d: %w", run.ID, err)
				}
			}
			return step.StepOutcome(evidence), nil
		}
		var err error
		if baseline, err = l.passBaseline(ctx, w, run, hasVerify); err != nil {
			return 0, fmt.Errorf("pass baseline for run %d: %w", run.ID, err)
		}
	}
	commits := 0
	if baseline != "" {
		var err error
		commits, err = l.commitsSinceBaseline(ctx, ticket, obs, plan.SHA(baseline))
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
	now := l.clock.Now()
	return slices.ContainsFunc(intents, func(i store.VerbIntent) bool {
		return i.TicketID == ticketURL && !i.HoldUntil.After(now)
	}), nil
}

const maxQuestionBytes = 4096

func (l *Loop) questionPath(runID plan.RunID) string {
	return filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.question", runID))
}

func (l *Loop) observeRuns(ctx context.Context, obs *plan.Observation) error {
	pending, err := l.store.PendingRunsAwaitingDisposition(ctx)
	if err != nil {
		return fmt.Errorf("list runs awaiting disposition: %w", err)
	}
	if obs.Runs == nil {
		obs.Runs = map[plan.TicketURL]plan.RunObservation{}
	}
	obs.Questions = map[plan.RunID]string{}
	now := l.clock.Now()
	for _, run := range pending {
		alive, err := l.runner.Liveness(run.Pgid, run.ProcStartedAt, now)
		if err != nil {
			return fmt.Errorf("liveness for run %d: %w", run.ID, err)
		}
		url := plan.TicketURL(run.TicketID)
		if alive {
			obs.MarkAlive(url)
			continue
		}
		obs.Runs[url] = plan.RunObservation{}
		id := plan.RunID(run.ID)
		question, wrote, err := l.readQuestionFile(id)
		if err != nil {
			return err
		}
		if wrote {
			obs.Questions[id] = question
		}
	}
	return nil
}

func (l *Loop) readQuestionFile(id plan.RunID) (question string, wrote bool, err error) {
	f, err := os.Open(l.questionPath(id))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("open question for run %d: %w", id, err)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxQuestionBytes))
	if closeErr := f.Close(); closeErr != nil {
		err = errors.Join(err, closeErr)
	}
	if err != nil {
		return "", false, fmt.Errorf("read question for run %d: %w", id, err)
	}
	return string(data), true, nil
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
	ctx context.Context, ticket store.Ticket, obs plan.Observation, baselineSHA plan.SHA,
) (int, error) {
	if worktreePath := obs.Worktrees[ticket.Ref()]; worktreePath != "" {
		commits, err := git.CommitsSince(ctx, worktreePath, string(baselineSHA), "HEAD")
		if err != nil {
			return 0, fmt.Errorf("count commits in %s: %w", worktreePath, err)
		}
		return commits, nil
	}
	tip, ok := obs.BranchTips[ticket.Ref()]
	if !ok {
		return 0, nil
	}
	repoPath := l.checkout(ticket.Repo)
	commits, err := git.CommitsSince(ctx, repoPath, string(baselineSHA), string(tip))
	if err != nil {
		return 0, fmt.Errorf("count commits on %s/%s: %w", ticket.Repo, ticket.Branch, err)
	}
	return commits, nil
}

func (l *Loop) launchEligible(
	ctx context.Context, snap plan.Snapshot, t *tick, byTicket map[plan.TicketURL]store.Ticket,
) error {
	toLaunch := snap.LaunchAfter(t.spawned)
	if len(toLaunch) == 0 {
		return nil
	}

	for _, ticketURL := range toLaunch {
		if slices.Contains(t.spawned, ticketURL) {
			continue
		}
		entry, _ := snap.Entry(ticketURL)
		var err error
		if entry.Run.IsSome() {
			err = l.advancePass(ctx, t, byTicket[ticketURL], entry)
		} else {
			err = l.cutAndSpawn(ctx, t, byTicket[ticketURL], entry.Ticket, entry.Unlock.BaseBranch, entry.PromptHash,
				l.startOf(entry, t.obs))
		}
		if err != nil {
			return fmt.Errorf("%s: %w", ticketURL, err)
		}
	}
	return nil
}

func (l *Loop) tickCheckingWaits(ctx context.Context, tickets []store.Ticket) error {
	urls := make([]plan.TicketURL, len(tickets))
	for i, t := range tickets {
		urls[i] = plan.TicketURL(t.URL)
	}
	if err := l.store.IncrementCheckingTicks(ctx, urls); err != nil {
		return fmt.Errorf("increment checking ticks: %w", err)
	}
	return nil
}

func ticketsByURL(tickets []store.Ticket) map[plan.TicketURL]store.Ticket {
	byURL := make(map[plan.TicketURL]store.Ticket, len(tickets))
	for _, t := range tickets {
		byURL[plan.TicketURL(t.URL)] = t
	}
	return byURL
}

func eachEntry(snap plan.Snapshot, keep func(plan.Entry) bool, fn func(plan.Entry) error) error {
	for _, entry := range snap.Entries {
		if !keep(entry) {
			continue
		}
		if err := fn(entry); err != nil {
			return fmt.Errorf("%s/%s: %w", entry.Ticket.Repo, entry.Ticket.Branch, err)
		}
	}
	return nil
}

func (l *Loop) cutAndSpawn(
	ctx context.Context, t *tick, ticket store.Ticket, launched plan.Ticket, baseBranch, promptHash string,
	begin start,
) error {
	branch := ticket.Branch
	repoPath := l.checkout(ticket.Repo)

	err := l.worktrees.New(ctx, repoPath, branch, "origin/"+baseBranch)
	if errors.Is(err, treepad.ErrPostHook) {
		l.logger.Warn("post hook failed after cutting worktree", "branch", branch, "err", err)
		err = nil
	}
	if err != nil {
		failed := store.RunSkeleton{
			Ticket: ticket.URL, PromptHash: promptHash,
			Step: begin.step.Name, Workflow: begin.workflow.Name, WorkflowHash: begin.workflow.Hash,
		}
		if _, insertErr := l.store.InsertCutFailedRun(ctx, failed, l.clock.Now()); insertErr != nil {
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
		return fmt.Errorf("cut of %s reported success but git worktree list does not show it", branch)
	}

	if err := l.spawnRun(ctx, t, spawnSpec{
		ticket: ticket, worktree: worktreePath, baseline: baselineSHA, hash: promptHash,
		kind: plan.RunKindAgent, prompt: stepPrompt(ticket, launched, begin.step, nil), choice: launched.Choice,
		workflow: begin.workflow, step: begin.step, verify: begin.verify,
	}); err != nil {
		return fmt.Errorf("spawn agent in %s: %w", worktreePath, err)
	}
	return nil
}

func agentPrompt(ticket store.Ticket, launched plan.Ticket) string {
	prompt := plan.Compose(launched)
	if ticket.Body != "" {
		prompt += "\n\n## Ticket\n\n" + ticket.Body
	}
	return prompt
}

type spawnSpec struct {
	ticket                           store.Ticket
	worktree, baseline, hash, prompt string
	kind                             plan.RunKind
	choice                           plan.AgentChoice
	resumeSessionID, message         string
	workflow                         plan.Workflow
	step                             plan.Step
	verify                           []string
	cause                            plan.FollowUpCause
}

func (spec spawnSpec) session() store.RunSession {
	switch {
	case spec.resumeSessionID != "":
		return store.RunSession{JoinID: spec.resumeSessionID, Message: spec.message}
	case spec.kind == plan.RunKindAgent || spec.kind == plan.RunKindFollowUp || spec.kind == plan.RunKindResolve:
		return store.RunSession{StartNew: true, Message: spec.message}
	default:
		return store.RunSession{}
	}
}

func (l *Loop) spawnRun(ctx context.Context, t *tick, spec spawnSpec) (err error) {
	ticket := spec.ticket
	if spec.workflow.Name == "" {
		spec.workflow = l.workflows[config.DefaultWorkflow]
		if spec.step.Name == "" {
			spec.step = spec.workflow.Steps[0]
		}
	}
	step := spec.step
	choice := spec.choice
	switch {
	case step.Command:
		if spec.kind != plan.RunKindVerify {
			spec.kind = plan.RunKindCommand
		}
		choice = plan.AgentChoice{}
	case step.Model != "":
		choice = plan.AgentChoice{Model: step.Model}
	case choice.Model == "":
		choice.Model = config.AgentModel
	}
	runID, sessionID, err := l.store.InsertSessionRunSkeleton(ctx, store.RunSkeleton{
		Ticket: ticket.URL, Kind: spec.kind, BaselineSHA: spec.baseline, PromptHash: spec.hash,
		Choice: choice, Session: spec.session(), Cause: spec.cause,
		Step: step.Name, Workflow: spec.workflow.Name, WorkflowHash: spec.workflow.Hash,
	})
	if err != nil {
		return fmt.Errorf("insert run skeleton for %s: %w", ticket.URL, err)
	}

	prompt := spec.prompt
	canAsk := len(step.Tools) == 0 || slices.Contains(step.Tools, "Write")
	if !step.Command && canAsk && (spec.kind == plan.RunKindAgent || spec.kind == plan.RunKindFollowUp) {
		prompt += plan.ComposeAsking(l.questionPath(plan.RunID(runID)))
	}
	promptPath := filepath.Join(l.ws.RunsDir, fmt.Sprintf("%d.prompt", runID))
	if !step.Command {
		if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
			return fmt.Errorf("write prompt for run %d: %w", runID, err)
		}
		if err := l.store.RecordPrompt(ctx, runID, prompt); err != nil {
			return fmt.Errorf("store prompt for run %d: %w", runID, err)
		}
	}
	systemPromptPath, err := l.systemPromptFor(runID, spec)
	if err != nil {
		return err
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
		Model:        choice.Model,
		Effort:       choice.Effort,
		Tools:        step.Tools,
		Prompt:       prompt,
		PromptPath:   promptPath,
		SessionID:    sessionID,
		Resume:       spec.resumeSessionID != "",
		LogFile:      logFile,
	}
	if spec.kind == plan.RunKindAgent || spec.kind == plan.RunKindResolve {
		spawnCfg.SystemPromptPath = systemPromptPath
	}
	if step.Command {
		spawnCfg = runner.SpawnConfig{AgentCommand: spec.verify, WorktreePath: spec.worktree, LogFile: logFile}
	}
	result, err := l.runner.Spawn(ctx, spawnCfg)
	if err != nil {
		failed := store.Disposition{
			RunID: plan.RunID(runID), Outcome: plan.Disposition(0),
			ExitCode: option.None[int](), Cost: option.None[float64](), EndedAt: l.clock.Now(),
		}
		if dispositionErr := l.store.RecordDisposition(ctx, failed); dispositionErr != nil {
			return errors.Join(
				fmt.Errorf("spawn run %d: %w", runID, err),
				fmt.Errorf("record disposition for failed spawn of run %d: %w", runID, dispositionErr),
			)
		}
		return l.event(ctx, ticket.URL, store.EventSpawnFailed, err.Error())
	}

	spawned := store.Spawn{RunID: plan.RunID(runID), Pgid: result.Pid, StartedAt: l.clock.Now(), LogPath: logPath}
	if err := l.store.RecordSpawn(ctx, spawned); err != nil {
		return fmt.Errorf("record spawn of run %d: %w", runID, err)
	}
	t.spawned = append(t.spawned, plan.TicketURL(ticket.URL))
	if spec.kind == plan.RunKindCommand || spec.kind == plan.RunKindVerify {
		t.spawnedCommands++
	}
	detail := fmt.Sprintf("spawned pid %d in %s", result.Pid, spec.worktree)
	if spec.baseline != "" {
		detail += " from " + spec.baseline
	}
	return l.event(ctx, ticket.URL, store.EventRunLaunched, detail)
}
