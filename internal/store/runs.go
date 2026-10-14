package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// InsertRunSkeleton reserves a runs row before the process exists; pgid and log_path
// land in a later RecordSpawn.
func (s *Store) InsertRunSkeleton(
	ctx context.Context, ticketID string, kind plan.RunKind, baselineSHA, promptHash string,
) (int64, error) {
	id, _, err := s.InsertSessionRunSkeleton(ctx, ticketID, kind, baselineSHA, promptHash, RunSession{})
	if err != nil {
		return 0, fmt.Errorf("insert run skeleton for %s: %w", ticketID, err)
	}
	return id, nil
}

type RunSession struct {
	StartNew bool
	JoinID   string
	Message  string
}

func (s *Store) InsertSessionRunSkeleton(
	ctx context.Context, ticketID string, kind plan.RunKind, baselineSHA, promptHash string, session RunSession,
) (runID int64, sessionID string, err error) {
	row, err := s.q.InsertRunSkeleton(ctx, ccdb.InsertRunSkeletonParams{
		TicketID:    notNull(ticketID),
		Kind:        string(kind),
		BaselineSHA: notNull(baselineSHA),
		PromptHash:  notNull(promptHash),
		NewSession:  session.StartNew,
		SessionID:   nullIfEmpty(session.JoinID),
		Message:     nullIfEmpty(session.Message),
	})
	if err != nil {
		return 0, "", fmt.Errorf("insert run skeleton for %s: %w", ticketID, err)
	}
	return row.ID, row.SessionID, nil
}

func (s *Store) LatestSessionID(ctx context.Context, ticketID string) (string, error) {
	id, err := s.q.LatestSessionID(ctx, notNull(ticketID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("select latest session of %s: %w", ticketID, err)
	}
	return id, nil
}

// RecordSpawn writes the pgid, its process start time and the log path onto a reserved run.
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

func (s *Store) RecordDisposition(
	ctx context.Context, runID int64, outcome plan.Outcome, exitCode *int, endedAt time.Time,
	metrics *agentlog.RunMetrics,
) error {
	var exitCodeParam sql.NullInt64
	if exitCode != nil {
		exitCodeParam = sql.NullInt64{Int64: int64(*exitCode), Valid: true}
	}
	err := s.q.RecordDisposition(ctx, ccdb.RecordDispositionParams{
		Outcome:  notNull(outcome.String()),
		ExitCode: exitCodeParam,
		EndedAt:  notNullTime(endedAt.UTC()),
		CostUsd:  costUSD(metrics),
		ID:       runID,
	})
	if err != nil {
		return fmt.Errorf("record disposition for run %d: %w", runID, err)
	}
	return nil
}

func (s *Store) RecordQuestion(ctx context.Context, runID int64, question string) error {
	err := s.q.RecordQuestion(ctx, ccdb.RecordQuestionParams{Question: notNull(question), ID: runID})
	if err != nil {
		return fmt.Errorf("record question for run %d: %w", runID, err)
	}
	return nil
}

func costUSD(metrics *agentlog.RunMetrics) sql.NullFloat64 {
	if metrics == nil || metrics.CostUSD == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *metrics.CostUSD, Valid: true}
}

// InsertCutFailedRun records a run that never got a worktree: no baseline, no pgid.
func (s *Store) InsertCutFailedRun(ctx context.Context, ticketID, promptHash string, at time.Time) (int64, error) {
	id, err := s.q.InsertCutFailedRun(ctx, ccdb.InsertCutFailedRunParams{
		TicketID:   notNull(ticketID),
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
	Kind          plan.RunKind
	Pgid          int
	ProcStartedAt time.Time
	BaselineSHA   string
	LogPath       string
}

// PendingRunsAwaitingDisposition returns every run with a pgid but no outcome yet.
func (s *Store) PendingRunsAwaitingDisposition(ctx context.Context) ([]PendingRun, error) {
	rows, err := s.q.PendingRunsAwaitingDisposition(ctx)
	if err != nil {
		return nil, fmt.Errorf("select pending runs: %w", err)
	}

	var pending []PendingRun
	for _, row := range rows {
		kind, err := plan.ParseRunKind(row.Kind)
		if err != nil {
			return nil, fmt.Errorf("read runs.kind of run %d: %w", row.ID, err)
		}
		p := PendingRun{
			ID: row.ID, TicketID: row.TicketID.String, Kind: kind, Pgid: int(row.Pgid.Int64),
			ProcStartedAt: row.ProcStartedAt.Time,
		}
		p.BaselineSHA, p.LogPath = row.BaselineSHA.String, row.LogPath.String
		pending = append(pending, p)
	}
	return pending, nil
}

// LatestRunsByTicket returns each ticket's most recent run (highest id).
func (s *Store) LatestRunsByTicket(ctx context.Context) (map[string]plan.RunSummary, error) {
	rows, err := s.q.LatestRunsByTicket(ctx)
	if err != nil {
		return nil, fmt.Errorf("select latest runs: %w", err)
	}

	summaries := map[string]plan.RunSummary{}
	for _, row := range rows {
		kind, err := plan.ParseRunKind(row.Kind)
		if err != nil {
			return nil, fmt.Errorf("read runs.kind of run %d: %w", row.ID, err)
		}
		summary := plan.RunSummary{ID: row.ID, Kind: kind}
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
			outcome, err := plan.ParseOutcome(row.Outcome.String)
			if err != nil {
				return nil, fmt.Errorf("read outcome of run %d: %w", row.ID, err)
			}
			summary.HasOutcome = true
			summary.Outcome = outcome
		}
		summary.BaselineSHA, summary.LogPath = row.BaselineSHA.String, row.LogPath.String
		summary.PromptHash = row.PromptHash.String
		summary.Question = row.Question.String
		summaries[row.TicketID] = summary
	}
	return summaries, nil
}

// VerbIntent is one queued action against a ticket. Payload is empty for verbs that carry none.
type VerbIntent struct {
	ID       int64
	TicketID string
	Payload  string
}

// QueueVerbIntent records one requested verb against a ticket.
func (s *Store) QueueVerbIntent(ctx context.Context, ticketID string, verb plan.Verb, at time.Time) error {
	err := s.q.QueueVerbIntent(ctx, ccdb.QueueVerbIntentParams{
		At:       at.UTC(),
		TicketID: ticketID,
		Verb:     string(verb),
	})
	if err != nil {
		return fmt.Errorf("queue %s intent for %s: %w", verb, ticketID, err)
	}
	return nil
}

// QueueVerbIntentWithPayload is QueueVerbIntent for a verb that carries an argument.
func (s *Store) QueueVerbIntentWithPayload(
	ctx context.Context, ticketID string, verb plan.Verb, payload string, at time.Time,
) error {
	err := s.q.QueueVerbIntentWithPayload(ctx, ccdb.QueueVerbIntentWithPayloadParams{
		At:       at.UTC(),
		TicketID: ticketID,
		Verb:     string(verb),
		Payload:  notNull(payload),
	})
	if err != nil {
		return fmt.Errorf("queue %s intent for %s: %w", verb, ticketID, err)
	}
	return nil
}

// PendingVerbIntents returns every unconsumed intent for verb, oldest first.
func (s *Store) PendingVerbIntents(ctx context.Context, verb plan.Verb) ([]VerbIntent, error) {
	rows, err := s.q.PendingVerbIntents(ctx, string(verb))
	if err != nil {
		return nil, fmt.Errorf("select %s intents: %w", verb, err)
	}

	var intents []VerbIntent
	for _, row := range rows {
		intents = append(intents, VerbIntent{ID: row.ID, TicketID: row.TicketID, Payload: row.Payload.String})
	}
	return intents, nil
}

// PendingIntentsByTicket returns every unconsumed intent, keyed by ticket, most recent last.
func (s *Store) PendingIntentsByTicket(ctx context.Context) (map[string][]plan.Verb, error) {
	rows, err := s.q.PendingIntentsByTicket(ctx)
	if err != nil {
		return nil, fmt.Errorf("select pending intents: %w", err)
	}

	byTicket := map[string][]plan.Verb{}
	for _, row := range rows {
		verb, err := plan.ParseVerb(row.Verb)
		if err != nil {
			return nil, fmt.Errorf("read intents.verb for %s: %w", row.TicketID, err)
		}
		byTicket[row.TicketID] = append(byTicket[row.TicketID], verb)
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

// ActiveLaunchHashes returns the authorised prompt hash per ticket in an active launch.
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

// RunIDsForTicket returns every run id recorded for a ticket, oldest first.
func (s *Store) RunIDsForTicket(ctx context.Context, ticketID string) ([]int64, error) {
	ids, err := s.q.RunIDsForTicket(ctx, notNull(ticketID))
	if err != nil {
		return nil, fmt.Errorf("select run ids for %s: %w", ticketID, err)
	}
	return ids, nil
}

type SessionRun struct {
	ID       int64
	LogPath  string
	Message  string
	Question string
}

func (s *Store) SessionRunsForTicket(ctx context.Context, ticketID string) ([]SessionRun, error) {
	rows, err := s.q.LatestSessionRuns(ctx, notNull(ticketID))
	if err != nil {
		return nil, fmt.Errorf("select session runs for %s: %w", ticketID, err)
	}
	runs := make([]SessionRun, len(rows))
	for i, row := range rows {
		runs[i] = SessionRun{ID: row.ID, LogPath: row.LogPath, Message: row.Message, Question: row.Question}
	}
	return runs, nil
}

// LatestRunLog returns the newest run's log path for a ticket and whether it has ended.
// A ticket with no run reads as ended with no path.
func (s *Store) LatestRunLog(ctx context.Context, ticketURL string) (path string, ended bool, err error) {
	row, err := s.q.LatestRunLog(ctx, notNull(ticketURL))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", true, nil
	case err != nil:
		return "", false, fmt.Errorf("select latest run log for %s: %w", ticketURL, err)
	default:
		return row.LogPath.String, row.EndedAt.Valid, nil
	}
}
