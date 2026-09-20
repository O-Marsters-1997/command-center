package cc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/cc/ccdb"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// InsertRunSkeleton reserves a runs row before the process exists: ticket_id, kind, baseline_sha
// and prompt_hash are known at cut time, but pgid and log_path are named after the row's own id
// (docs/prds/prd-command-centre.md § A run), so they land in a later RecordSpawn.
func (s *Store) InsertRunSkeleton(ctx context.Context, ticketID, kind, baselineSHA, promptHash string) (int64, error) {
	id, err := s.q.InsertRunSkeleton(ctx, ccdb.InsertRunSkeletonParams{
		TicketID:    ticketID,
		Kind:        kind,
		BaselineSHA: notNull(baselineSHA),
		PromptHash:  notNull(promptHash),
	})
	if err != nil {
		return 0, fmt.Errorf("insert run skeleton for %s: %w", ticketID, err)
	}
	return id, nil
}

// RecordSpawn writes the pgid, its process start time and the log path onto a reserved run row,
// in the one UPDATE that must happen immediately after Spawn returns and nothing else.
func (s *Store) RecordSpawn(ctx context.Context, runID int64, pgid int, startedAt time.Time, logPath string) error {
	err := s.q.RecordSpawn(ctx, ccdb.RecordSpawnParams{
		Pgid:          sql.NullInt64{Int64: int64(pgid), Valid: true},
		ProcStartedAt: notNullTime(startedAt.UTC()),
		LogPath:       notNull(logPath),
		ID:            runID,
	})
	if err != nil {
		return fmt.Errorf("record spawn for run %d: %w", runID, err)
	}
	return nil
}

// RecordDisposition writes a dead run's outcome and its metrics in the same UPDATE. exitCode and
// metrics are both nil when there is nothing to report: a spawn failure, an unreapable process,
// or no log to parse.
func (s *Store) RecordDisposition(
	ctx context.Context, runID int64, outcome plan.Outcome, exitCode *int, endedAt time.Time,
	metrics *agentlog.RunMetrics,
) error {
	var exitCodeParam sql.NullInt64
	if exitCode != nil {
		exitCodeParam = sql.NullInt64{Int64: int64(*exitCode), Valid: true}
	}
	m := runMetricsColumns(metrics)
	err := s.q.RecordDisposition(ctx, ccdb.RecordDispositionParams{
		Outcome:        notNull(outcome.String()),
		ExitCode:       exitCodeParam,
		EndedAt:        notNullTime(endedAt.UTC()),
		TokensIn:       m.TokensIn,
		TokensOut:      m.TokensOut,
		Turns:          m.Turns,
		DurationMs:     m.DurationMs,
		CostUsd:        m.CostUsd,
		ToolCalls:      m.ToolCalls,
		ToolFailures:   m.ToolFailures,
		Model:          m.Model,
		MetricsSettled: m.MetricsSettled,
		ID:             runID,
	})
	if err != nil {
		return fmt.Errorf("record disposition for run %d: %w", runID, err)
	}
	return nil
}

type runMetricsCols struct {
	TokensIn, TokensOut, Turns, DurationMs, ToolCalls, ToolFailures sql.NullInt64
	CostUsd                                                         sql.NullFloat64
	Model                                                           sql.NullString
	MetricsSettled                                                  sql.NullBool
}

// runMetricsColumns zero-values every field when metrics is nil, which database/sql writes as NULL.
func runMetricsColumns(metrics *agentlog.RunMetrics) runMetricsCols {
	if metrics == nil {
		return runMetricsCols{}
	}
	var costUSD sql.NullFloat64
	if metrics.CostUSD != nil {
		costUSD = sql.NullFloat64{Float64: *metrics.CostUSD, Valid: true}
	}
	return runMetricsCols{
		TokensIn:       sql.NullInt64{Int64: metrics.TokensIn, Valid: true},
		TokensOut:      sql.NullInt64{Int64: metrics.TokensOut, Valid: true},
		Turns:          sql.NullInt64{Int64: int64(metrics.Turns), Valid: true},
		DurationMs:     sql.NullInt64{Int64: metrics.Duration.Milliseconds(), Valid: true},
		CostUsd:        costUSD,
		ToolCalls:      sql.NullInt64{Int64: int64(metrics.ToolCalls), Valid: true},
		ToolFailures:   sql.NullInt64{Int64: int64(metrics.ToolFailures), Valid: true},
		Model:          sql.NullString{String: metrics.Model, Valid: metrics.Model != ""},
		MetricsSettled: sql.NullBool{Bool: metrics.Settled, Valid: true},
	}
}

// RunAwaitingMetricsBackfill is one disposed run a backfill pass must try: it has a log path but
// no metrics written yet.
type RunAwaitingMetricsBackfill struct {
	ID      int64
	LogPath string
}

// RunsAwaitingMetricsBackfill returns every run with a log_path but no metrics written yet. A run
// already resolved -- settled true or false -- is excluded, which is what makes a second backfill
// pass safe to run.
func (s *Store) RunsAwaitingMetricsBackfill(ctx context.Context) ([]RunAwaitingMetricsBackfill, error) {
	rows, err := s.q.RunsAwaitingMetricsBackfill(ctx)
	if err != nil {
		return nil, fmt.Errorf("select runs awaiting metrics backfill: %w", err)
	}
	var runs []RunAwaitingMetricsBackfill
	for _, row := range rows {
		runs = append(runs, RunAwaitingMetricsBackfill{ID: row.ID, LogPath: row.LogPath.String})
	}
	return runs, nil
}

// BackfillRunMetrics writes one run's metrics columns alone, leaving outcome, exit_code and
// ended_at -- already written at disposition -- untouched.
func (s *Store) BackfillRunMetrics(ctx context.Context, runID int64, metrics agentlog.RunMetrics) error {
	m := runMetricsColumns(&metrics)
	err := s.q.BackfillRunMetrics(ctx, ccdb.BackfillRunMetricsParams{
		TokensIn:       m.TokensIn,
		TokensOut:      m.TokensOut,
		Turns:          m.Turns,
		DurationMs:     m.DurationMs,
		CostUsd:        m.CostUsd,
		ToolCalls:      m.ToolCalls,
		ToolFailures:   m.ToolFailures,
		Model:          m.Model,
		MetricsSettled: m.MetricsSettled,
		ID:             runID,
	})
	if err != nil {
		return fmt.Errorf("backfill metrics for run %d: %w", runID, err)
	}
	return nil
}

// InsertCutFailedRun records a run that never got a worktree, in one INSERT: no baseline, no
// pgid, ever (docs/prds/prd-command-centre.md § The states, cut failed).
func (s *Store) InsertCutFailedRun(ctx context.Context, ticketID, promptHash string, at time.Time) (int64, error) {
	id, err := s.q.InsertCutFailedRun(ctx, ccdb.InsertCutFailedRunParams{
		TicketID:   ticketID,
		PromptHash: notNull(promptHash),
		Outcome:    notNull(plan.OutcomeCutFailed.String()),
		EndedAt:    notNullTime(at.UTC()),
	})
	if err != nil {
		return 0, fmt.Errorf("insert cut-failed run for %s: %w", ticketID, err)
	}
	return id, nil
}

// PendingRun is one run this tick must check for liveness and, if it has died, dispose of.
type PendingRun struct {
	ID            int64
	TicketID      string
	Pgid          int
	ProcStartedAt time.Time
	BaselineSHA   string
	LogPath       string
}

// PendingRunsAwaitingDisposition returns every run with a pgid but no outcome yet: the exact
// set the loop's liveness+disposition pass must check, every tick, restart or not.
func (s *Store) PendingRunsAwaitingDisposition(ctx context.Context) ([]PendingRun, error) {
	rows, err := s.q.PendingRunsAwaitingDisposition(ctx)
	if err != nil {
		return nil, fmt.Errorf("select pending runs: %w", err)
	}

	var pending []PendingRun
	for _, row := range rows {
		p := PendingRun{
			ID: row.ID, TicketID: row.TicketID, Pgid: int(row.Pgid.Int64),
			ProcStartedAt: row.ProcStartedAt.Time,
		}
		p.BaselineSHA, p.LogPath = row.BaselineSHA.String, row.LogPath.String
		pending = append(pending, p)
	}
	return pending, nil
}

// RunSummary is the latest run the page and the launch-eligibility check need per ticket.
type RunSummary struct {
	ID            int64
	Pgid          *int
	ProcStartedAt *time.Time
	HasOutcome    bool
	Outcome       plan.Outcome
	ExitCode      *int
	EndedAt       *time.Time
	LogPath       string
	BaselineSHA   string
	// PromptHash is what this run was spawned (or cut-failed) against — the page's own prompt-
	// changed comparison for a row that has already run compares a fresh recomposition against
	// this, never the launch membership's hash, which a later re-run or relaunch supersedes.
	PromptHash string
	// Kind is runs.kind: "agent" for a launch or re-run, "resolve" for a conflict-resolution run.
	Kind string
}

// LatestRunsByTicket returns each ticket's single most recent run (highest id). Its presence alone
// is what LaunchPlan's "no prior run" rule and the page's pgid/elapsed/log-path columns need.
func (s *Store) LatestRunsByTicket(ctx context.Context) (map[string]RunSummary, error) {
	rows, err := s.q.LatestRunsByTicket(ctx)
	if err != nil {
		return nil, fmt.Errorf("select latest runs: %w", err)
	}

	summaries := map[string]RunSummary{}
	for _, row := range rows {
		summary := RunSummary{ID: row.ID, Kind: row.Kind}
		if row.Pgid.Valid {
			v := int(row.Pgid.Int64)
			summary.Pgid = &v
		}
		if row.ProcStartedAt.Valid {
			summary.ProcStartedAt = &row.ProcStartedAt.Time
		}
		if row.EndedAt.Valid {
			summary.EndedAt = &row.EndedAt.Time
		}
		if row.ExitCode.Valid {
			v := int(row.ExitCode.Int64)
			summary.ExitCode = &v
		}
		if row.Outcome.Valid {
			summary.HasOutcome = true
			summary.Outcome = outcomeFromString(row.Outcome.String)
		}
		summary.BaselineSHA, summary.LogPath = row.BaselineSHA.String, row.LogPath.String
		summary.PromptHash = row.PromptHash.String
		summaries[row.TicketID] = summary
	}
	return summaries, nil
}

func outcomeFromString(s string) plan.Outcome {
	switch s {
	case plan.OutcomePush.String():
		return plan.OutcomePush
	case plan.OutcomeCutFailed.String():
		return plan.OutcomeCutFailed
	default:
		return plan.OutcomeFailed
	}
}

// VerbIntent is one queued action against a ticket — the loop's own read-then-act sequence, never
// a handler's (see server.go's handleVerb). Payload is empty for every verb that carries none.
type VerbIntent struct {
	ID       int64
	TicketID string
	Payload  string
}

// QueueVerbIntent records one requested verb against a ticket. A handler only ever does this one
// blind INSERT; the loop is the sole reader and actor (inv. 9).
func (s *Store) QueueVerbIntent(ctx context.Context, ticketID, verb string, at time.Time) error {
	err := s.q.QueueVerbIntent(ctx, ccdb.QueueVerbIntentParams{
		At:       at.UTC(),
		TicketID: ticketID,
		Verb:     verb,
	})
	if err != nil {
		return fmt.Errorf("queue %s intent for %s: %w", verb, ticketID, err)
	}
	return nil
}

// QueueVerbIntentWithPayload is QueueVerbIntent's sibling for a verb that carries its own
// argument, such as follow-up's typed prompt text.
func (s *Store) QueueVerbIntentWithPayload(ctx context.Context, ticketID, verb, payload string, at time.Time) error {
	err := s.q.QueueVerbIntentWithPayload(ctx, ccdb.QueueVerbIntentWithPayloadParams{
		At:       at.UTC(),
		TicketID: ticketID,
		Verb:     verb,
		Payload:  notNull(payload),
	})
	if err != nil {
		return fmt.Errorf("queue %s intent for %s: %w", verb, ticketID, err)
	}
	return nil
}

// PendingVerbIntents returns every unconsumed intent for verb, oldest first.
func (s *Store) PendingVerbIntents(ctx context.Context, verb string) ([]VerbIntent, error) {
	rows, err := s.q.PendingVerbIntents(ctx, verb)
	if err != nil {
		return nil, fmt.Errorf("select %s intents: %w", verb, err)
	}

	var intents []VerbIntent
	for _, row := range rows {
		intents = append(intents, VerbIntent{ID: row.ID, TicketID: row.TicketID, Payload: row.Payload.String})
	}
	return intents, nil
}

// PendingIntentsByTicket is every unconsumed intent, keyed by ticket, most recent last.
func (s *Store) PendingIntentsByTicket(ctx context.Context) (map[string][]string, error) {
	rows, err := s.q.PendingIntentsByTicket(ctx)
	if err != nil {
		return nil, fmt.Errorf("select pending intents: %w", err)
	}

	byTicket := map[string][]string{}
	for _, row := range rows {
		byTicket[row.TicketID] = append(byTicket[row.TicketID], row.Verb)
	}
	return byTicket, nil
}

// ConsumeVerbIntent marks one intent consumed, so a later tick never applies it again.
func (s *Store) ConsumeVerbIntent(ctx context.Context, id int64, at time.Time) error {
	err := s.q.ConsumeVerbIntent(ctx, ccdb.ConsumeVerbIntentParams{
		ConsumedAt: notNullTime(at.UTC()),
		ID:         id,
	})
	if err != nil {
		return fmt.Errorf("consume intent %d: %w", id, err)
	}
	return nil
}

// ActiveLaunchHashes returns the authorised prompt hash per ticket, for every ticket in an active
// launch — what the loop's launch-eligibility check compares the recomposed hash against.
func (s *Store) ActiveLaunchHashes(ctx context.Context) (map[string]string, error) {
	rows, err := s.q.ActiveLaunchHashes(ctx)
	if err != nil {
		return nil, fmt.Errorf("select active launch hashes: %w", err)
	}

	hashes := map[string]string{}
	for _, row := range rows {
		hashes[row.TicketID] = row.PromptHash
	}
	return hashes, nil
}

// RunIDsForTicket returns every run id ever recorded for a ticket, oldest first -- what
// remove-worktree's log pruning needs to find every runs/<id>.jsonl, runs/<id>.prompt and
// runs/<id>.diff it left behind (docs/prds/prd-command-centre.md § Phase 6).
func (s *Store) RunIDsForTicket(ctx context.Context, ticketID string) ([]int64, error) {
	ids, err := s.q.RunIDsForTicket(ctx, ticketID)
	if err != nil {
		return nil, fmt.Errorf("select run ids for %s: %w", ticketID, err)
	}
	return ids, nil
}

// LatestRunLog returns the newest run's log path for a ticket and whether that run has ended. A
// ticket with no run at all reads as ended with no path, so a caller tailing the log streams
// nothing rather than failing.
func (s *Store) LatestRunLog(ctx context.Context, ticketURL string) (path string, ended bool, err error) {
	row, err := s.q.LatestRunLog(ctx, ticketURL)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", true, nil
	case err != nil:
		return "", false, fmt.Errorf("select latest run log for %s: %w", ticketURL, err)
	default:
		return row.LogPath.String, row.EndedAt.Valid, nil
	}
}
