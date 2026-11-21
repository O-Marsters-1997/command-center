package loop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
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

func (l *Loop) baseline(ctx context.Context, ticket store.Ticket) (string, error) {
	sha, err := git.BranchTip(ctx, l.checkout(ticket.Repo), ticket.Branch)
	if err != nil {
		return "", fmt.Errorf("read baseline for %s: %w", ticket.URL, err)
	}
	return sha, nil
}

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

func (l *Loop) eachIntentEntry(
	ctx context.Context, snap plan.Snapshot, verb plan.Verb, fn func(plan.Intent, plan.Entry) error,
) error {
	return l.eachIntent(ctx, snap, verb, func(intent plan.Intent) error {
		e, ok := snap.Entry(intent.Ticket)
		if !ok {
			return nil
		}
		if err := fn(intent, e); err != nil {
			return fmt.Errorf("%s/%s: %w", e.Ticket.Repo, e.Ticket.Branch, err)
		}
		return nil
	})
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
		return l.failTick(ctx, fmt.Errorf("observe: %w", err))
	}

	timing.Repos = observedRepos(obs)
	for _, run := range obs.Runs {
		if run.Alive {
			timing.LiveCommandRuns++
		}
	}

	now := l.clock.Now()
	t := &tick{obs: obs, cancelled: map[int]bool{}, timing: &timing}
	t.obs.ObservedAt = now
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
	return l.failTick(ctx, l.act(ctx, snap.Skipping(), t, byURL))
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
	if err := l.restartWaitsOnNewHeads(ctx, tickets, t); err != nil {
		return nil, fmt.Errorf("restart checking waits: %w", err)
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
	if err := l.applyAdoptHeadIntents(ctx, snap, t.obs); err != nil {
		return fmt.Errorf("adopt head: %w", err)
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
	pushable := snap.Skipping()
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
	in.Intents, in.Held = splitHeld(in.Intents, in.Now)
	in.Obs = t.obs
	in.Prev = t.prev
	return l.rules(t.obs).Derive(in), nil
}

func splitHeld(
	byTicket map[plan.TicketURL][]plan.Intent, now time.Time,
) (released, held map[plan.TicketURL][]plan.Intent) {
	released = make(map[plan.TicketURL][]plan.Intent, len(byTicket))
	held = make(map[plan.TicketURL][]plan.Intent)
	for url, intents := range byTicket {
		for _, i := range intents {
			if i.HeldAt(now) {
				held[url] = append(held[url], i)
			} else {
				released[url] = append(released[url], i)
			}
		}
	}
	return released, held
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

func (l *Loop) restartWaitsOnNewHeads(ctx context.Context, tickets []store.Ticket, t *tick) error {
	for _, tk := range tickets {
		ref := plan.BranchRef{Repo: tk.Repo, Branch: tk.Branch}
		if !t.obs.HeadDescends[ref] || t.obs.PRs[ref].HeadOid == t.prev.PRs[ref].HeadOid {
			continue
		}
		if err := l.store.ResetCheckingTicks(ctx, plan.TicketURL(tk.URL)); err != nil {
			return fmt.Errorf("reset checking ticks for %s: %w", tk.URL, err)
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
