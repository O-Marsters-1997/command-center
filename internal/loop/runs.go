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
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

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
		if cancelled[url] || !ok || !run.Kind.Steerable() || run.Pgid.IsNone() || run.Outcome.IsSome() {
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
	for _, run := range pending {
		url := plan.TicketURL(run.TicketID)
		observed, seen := t.obs.Runs[url]
		if !seen || observed.Alive {
			if seen && !t.cancelled[run.Pgid] {
				continue
			}
			alive, err := l.observeRun(run, &t.obs, l.clock.Now())
			if err != nil {
				return err
			}
			if alive {
				continue
			}
		}
		if err := l.disposeRun(ctx, run, byTicket[url], t.obs); err != nil {
			return fmt.Errorf("dispose run %d of %s: %w", run.ID, run.TicketID, err)
		}
	}
	return nil
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
		if _, err := l.observeRun(run, obs, now); err != nil {
			return err
		}
	}
	return nil
}

func (l *Loop) observeRun(run store.PendingRun, obs *plan.Observation, now time.Time) (alive bool, err error) {
	alive, err = l.runner.Liveness(run.Pgid, run.ProcStartedAt, now)
	if err != nil {
		return false, fmt.Errorf("liveness for run %d: %w", run.ID, err)
	}
	url := plan.TicketURL(run.TicketID)
	if alive {
		obs.MarkAlive(url)
		return true, nil
	}
	obs.Runs[url] = plan.RunObservation{}
	id := plan.RunID(run.ID)
	question, wrote, err := l.readQuestionFile(id)
	if err != nil {
		return false, err
	}
	if wrote {
		obs.Questions[id] = question
	}
	return false, nil
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
	if run.Kind.Steerable() {
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
		if !finalStep(w, step) {
			evidence := plan.StepEvidence{ExitCode: exitCode, Handoff: lastMessage}
			if !step.Command && !step.Writes && baseline != "" {
				var err error
				if evidence.Commits, err = l.runCommits(ctx, run, ticket, obs, baseline); err != nil {
					return 0, err
				}
			}
			return step.StepOutcome(evidence), nil
		}
		hasVerify := len(obs.Settings[ticket.Repo].VerifyCommand) > 0
		var err error
		if baseline, err = l.passBaseline(ctx, w, run, hasVerify); err != nil {
			return 0, fmt.Errorf("pass baseline for run %d: %w", run.ID, err)
		}
	}
	commits := 0
	if baseline != "" {
		var err error
		if commits, err = l.runCommits(ctx, run, ticket, obs, baseline); err != nil {
			return 0, err
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

func (l *Loop) runCommits(
	ctx context.Context, run store.PendingRun, ticket store.Ticket, obs plan.Observation, baseline string,
) (int, error) {
	commits, err := l.commitsSinceBaseline(ctx, ticket, obs, plan.SHA(baseline))
	if err != nil {
		return 0, fmt.Errorf("commits since baseline for run %d: %w", run.ID, err)
	}
	return commits, nil
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
