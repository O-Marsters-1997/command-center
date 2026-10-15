package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

// InsertRunSkeleton reserves a run before its process exists; RecordSpawn fills in its process details.
func (s *Store) InsertRunSkeleton(ctx context.Context, r RunSkeleton) (int64, error) {
	id, _, err := s.InsertSessionRunSkeleton(ctx, r)
	if err != nil {
		return 0, fmt.Errorf("insert run skeleton for %s: %w", r.Ticket, err)
	}
	return id, nil
}

type RunSession struct {
	StartNew bool
	JoinID   string
	Message  string
}

func (s *Store) InsertSessionRunSkeleton(
	ctx context.Context, r RunSkeleton,
) (runID int64, sessionID string, err error) {
	row, err := s.q.InsertRunSkeleton(ctx, ccdb.InsertRunSkeletonParams{
		TicketID:    notNull(r.Ticket),
		Kind:        string(r.Kind),
		BaselineSHA: notNull(r.BaselineSHA),
		PromptHash:  notNull(r.PromptHash),
		NewSession:  r.Session.StartNew,
		SessionID:   nullIfEmpty(r.Session.JoinID),
		Message:     nullIfEmpty(r.Session.Message),
	})
	if err != nil {
		return 0, "", fmt.Errorf("insert run skeleton for %s: %w", r.Ticket, err)
	}
	return row.ID, row.SessionID, nil
}

func (s *Store) LatestSessionID(ctx context.Context, ticketID string) (string, error) {
	id, err := s.q.LatestSessionID(ctx, notNull(ticketID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("session of %s: %w", ticketID, ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("select latest session of %s: %w", ticketID, err)
	}
	return id, nil
}

func (s *Store) RecordSpawn(ctx context.Context, sp Spawn) error {
	n, err := s.q.RecordSpawn(ctx, ccdb.RecordSpawnParams{
		Pgid:          sql.NullInt64{Int64: int64(sp.Pgid), Valid: true},
		ProcStartedAt: notNullTime(sp.StartedAt.UTC()),
		LogPath:       notNull(sp.LogPath),
		ID:            int64(sp.RunID),
	})
	return matchedRow(n, err, fmt.Sprintf("record spawn for run %d", sp.RunID))
}

func (s *Store) RecordDisposition(ctx context.Context, d Disposition) error {
	var exitCodeParam sql.NullInt64
	if d.ExitCode.IsSome() {
		exitCodeParam = sql.NullInt64{Int64: int64(d.ExitCode.Unwrap()), Valid: true}
	}
	var costParam sql.NullFloat64
	if d.Cost.IsSome() {
		costParam = sql.NullFloat64{Float64: d.Cost.Unwrap(), Valid: true}
	}
	n, err := s.q.RecordDisposition(ctx, ccdb.RecordDispositionParams{
		Outcome:  notNull(d.Outcome.String()),
		ExitCode: exitCodeParam,
		EndedAt:  notNullTime(d.EndedAt.UTC()),
		CostUsd:  costParam,
		ID:       int64(d.RunID),
	})
	return matchedRow(n, err, fmt.Sprintf("record disposition for run %d", d.RunID))
}

func (s *Store) RecordQuestion(ctx context.Context, runID int64, question string) error {
	n, err := s.q.RecordQuestion(ctx, ccdb.RecordQuestionParams{Question: notNull(question), ID: runID})
	return matchedRow(n, err, fmt.Sprintf("record question for run %d", runID))
}

// InsertCutFailedRun records a run that never got a worktree, baseline or process.
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

// PendingRun is a run awaiting a liveness check and disposition.
type PendingRun struct {
	ID            int64
	TicketID      string
	Kind          plan.RunKind
	Pgid          int
	ProcStartedAt time.Time
	BaselineSHA   string
	LogPath       string
}

// PendingRunsAwaitingDisposition excludes runs without a process or with an outcome.
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

// LatestRunsByTicket selects the highest run ID per ticket.
func (s *Store) LatestRunsByTicket(ctx context.Context) (map[plan.TicketURL]plan.RunSummary, error) {
	rows, err := s.q.ListLatestRunsByTicket(ctx)
	if err != nil {
		return nil, fmt.Errorf("select latest runs: %w", err)
	}

	summaries := map[plan.TicketURL]plan.RunSummary{}
	for _, row := range rows {
		kind, err := plan.ParseRunKind(row.Kind)
		if err != nil {
			return nil, fmt.Errorf("read runs.kind of run %d: %w", row.ID, err)
		}
		summary := plan.RunSummary{ID: plan.RunID(row.ID), Kind: kind}
		if row.Pgid.Valid {
			summary.Pgid = option.Some(int(row.Pgid.Int64))
		}
		if row.ProcStartedAt.Valid {
			summary.ProcStartedAt = option.Some(row.ProcStartedAt.Time)
		}
		if row.EndedAt.Valid {
			summary.EndedAt = option.Some(row.EndedAt.Time)
		}
		if row.ExitCode.Valid {
			summary.ExitCode = option.Some(int(row.ExitCode.Int64))
		}
		if row.Outcome.Valid {
			outcome, err := plan.ParseOutcome(row.Outcome.String)
			if err != nil {
				return nil, fmt.Errorf("read outcome of run %d: %w", row.ID, err)
			}
			summary.Outcome = option.Some(outcome)
		}
		summary.BaselineSHA, summary.LogPath = plan.SHA(row.BaselineSHA.String), row.LogPath.String
		summary.PromptHash = row.PromptHash.String
		summary.Question = row.Question.String
		summaries[plan.TicketURL(row.TicketID)] = summary
	}
	return summaries, nil
}

// VerbIntent holds a queued ticket action; verbs without arguments have an empty payload.
type VerbIntent struct {
	ID       int64
	TicketID string
	Payload  string
}

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

func (s *Store) QueueVerbIntentWithPayload(ctx context.Context, v VerbRequest) error {
	encoded, err := json.Marshal(v.Payload)
	if err != nil {
		return fmt.Errorf("encode %s intent payload for %s: %w", v.Verb, v.Ticket, err)
	}
	err = s.q.QueueVerbIntentWithPayload(ctx, ccdb.QueueVerbIntentWithPayloadParams{
		At:       v.At.UTC(),
		TicketID: v.Ticket,
		Verb:     string(v.Verb),
		Payload:  encoded,
	})
	if err != nil {
		return fmt.Errorf("queue %s intent for %s: %w", v.Verb, v.Ticket, err)
	}
	return nil
}

// PendingVerbIntents returns unconsumed intents for a verb in creation order.
func (s *Store) PendingVerbIntents(ctx context.Context, verb plan.Verb) ([]VerbIntent, error) {
	rows, err := s.q.PendingVerbIntents(ctx, string(verb))
	if err != nil {
		return nil, fmt.Errorf("select %s intents: %w", verb, err)
	}

	var intents []VerbIntent
	for _, row := range rows {
		var payload string
		if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode %s intent payload for %s: %w", verb, row.TicketID, err)
		}
		intents = append(intents, VerbIntent{ID: row.ID, TicketID: row.TicketID, Payload: payload})
	}
	return intents, nil
}

// PendingIntentsByTicket groups unconsumed intents by ticket in creation order.
func (s *Store) PendingIntentsByTicket(ctx context.Context) (map[plan.TicketURL][]plan.Verb, error) {
	rows, err := s.q.PendingIntentsByTicket(ctx)
	if err != nil {
		return nil, fmt.Errorf("select pending intents: %w", err)
	}

	byTicket := map[plan.TicketURL][]plan.Verb{}
	for _, row := range rows {
		verb, err := plan.ParseVerb(row.Verb)
		if err != nil {
			return nil, fmt.Errorf("read intents.verb for %s: %w", row.TicketID, err)
		}
		url := plan.TicketURL(row.TicketID)
		byTicket[url] = append(byTicket[url], verb)
	}
	return byTicket, nil
}

func (s *Store) ConsumeVerbIntent(ctx context.Context, id int64, at time.Time) error {
	n, err := s.q.ConsumeVerbIntent(ctx, ccdb.ConsumeVerbIntentParams{
		ConsumedAt: notNullTime(at.UTC()),
		ID:         id,
	})
	return matchedRow(n, err, fmt.Sprintf("consume intent %d", id))
}

// ActiveLaunchHashes excludes cancelled launches.
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

// RunIDsForTicket orders run IDs oldest first.
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

func (s *Store) LatestRunLog(ctx context.Context, ticketURL string) (path string, ended bool, err error) {
	row, err := s.q.LatestRunLog(ctx, notNull(ticketURL))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, fmt.Errorf("run of %s: %w", ticketURL, ErrNotFound)
	case err != nil:
		return "", false, fmt.Errorf("select latest run log for %s: %w", ticketURL, err)
	default:
		return row.LogPath.String, row.EndedAt.Valid, nil
	}
}
