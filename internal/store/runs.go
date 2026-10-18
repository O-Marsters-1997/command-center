package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
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

type RunDetails struct {
	SessionID string
	Message   string
	Outcome   option.Option[plan.Outcome]
}

func (s *Store) RunDetails(ctx context.Context, id plan.RunID) (RunDetails, error) {
	row, err := s.q.RunDetails(ctx, int64(id))
	if errors.Is(err, sql.ErrNoRows) {
		return RunDetails{}, fmt.Errorf("run %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return RunDetails{}, fmt.Errorf("read run %d: %w", id, err)
	}
	details := RunDetails{SessionID: row.SessionID, Message: row.Message}
	if row.Outcome != "" {
		outcome, err := plan.ParseOutcome(row.Outcome)
		if err != nil {
			return RunDetails{}, fmt.Errorf("read outcome of run %d: %w", id, err)
		}
		details.Outcome = option.Some(outcome)
	}
	return details, nil
}

func (s *Store) InsertSessionRunSkeleton(
	ctx context.Context, r RunSkeleton,
) (runID int64, sessionID string, err error) {
	model, effort := choiceColumns(r.Choice)
	row, err := s.q.InsertRunSkeleton(ctx, ccdb.InsertRunSkeletonParams{
		Model:        model,
		Effort:       effort,
		TicketID:     notNull(r.Ticket),
		Kind:         string(r.Kind),
		BaselineSHA:  notNull(r.BaselineSHA),
		PromptHash:   notNull(r.PromptHash),
		Step:         string(r.Step),
		Workflow:     r.Workflow,
		WorkflowHash: r.WorkflowHash,
		NewSession:   r.Session.StartNew,
		SessionID:    nullIfEmpty(r.Session.JoinID),
		Message:      nullIfEmpty(r.Session.Message),
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
	var lastMessageParam sql.NullString
	if d.LastMessage.IsSome() {
		lastMessageParam = notNull(d.LastMessage.Unwrap())
	}
	var metricsParam json.RawMessage
	if d.Metrics.IsSome() {
		raw, err := json.Marshal(d.Metrics.Unwrap())
		if err != nil {
			return fmt.Errorf("encode metrics for run %d: %w", d.RunID, err)
		}
		metricsParam = raw
	}
	n, err := s.q.RecordDisposition(ctx, ccdb.RecordDispositionParams{
		Metrics:     metricsParam,
		Outcome:     notNull(d.Outcome.String()),
		ExitCode:    exitCodeParam,
		EndedAt:     notNullTime(d.EndedAt.UTC()),
		CostUsd:     costParam,
		LastMessage: lastMessageParam,
		ID:          int64(d.RunID),
	})
	return matchedRow(n, err, fmt.Sprintf("record disposition for run %d", d.RunID))
}

func (s *Store) RecordQuestion(ctx context.Context, runID int64, question string) error {
	n, err := s.q.RecordQuestion(ctx, ccdb.RecordQuestionParams{Question: notNull(question), ID: runID})
	return matchedRow(n, err, fmt.Sprintf("record question for run %d", runID))
}

// InsertCutFailedRun records a run that never got a worktree, baseline or process.
func (s *Store) InsertCutFailedRun(ctx context.Context, r RunSkeleton, at time.Time) (int64, error) {
	id, err := s.q.InsertCutFailedRun(ctx, ccdb.InsertCutFailedRunParams{
		TicketID:     notNull(r.Ticket),
		PromptHash:   notNull(r.PromptHash),
		Step:         string(r.Step),
		Workflow:     r.Workflow,
		WorkflowHash: r.WorkflowHash,
		Outcome:      notNull(plan.OutcomeCutFailed.String()),
		EndedAt:      notNullTime(at.UTC()),
	})
	if err != nil {
		return 0, fmt.Errorf("insert cut-failed run for %s: %w", r.Ticket, err)
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
		summary.Step, summary.WorkflowHash = plan.StepName(row.Step), row.WorkflowHash
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

func (s *Store) QueueVerbIntent(ctx context.Context, v VerbRequest) error {
	var encoded []byte
	if v.Payload != "" {
		var err error
		encoded, err = json.Marshal(v.Payload)
		if err != nil {
			return fmt.Errorf("encode %s intent payload for %s: %w", v.Verb, v.Ticket, err)
		}
	}
	err := s.q.QueueVerbIntent(ctx, ccdb.QueueVerbIntentParams{
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
		if verb == plan.VerbLaunch {
			payload = string(row.Payload)
		} else if err := json.Unmarshal(row.Payload, &payload); err != nil {
			return nil, fmt.Errorf("decode %s intent payload for %s: %w", verb, row.TicketID, err)
		}
		intents = append(intents, VerbIntent{ID: row.ID, TicketID: row.TicketID, Payload: payload})
	}
	return intents, nil
}

func (s *Store) PendingIntents(ctx context.Context) (map[plan.TicketURL][]plan.Intent, error) {
	byTicket := map[plan.TicketURL][]plan.Intent{}
	for _, verb := range plan.AllVerbs() {
		pending, err := s.PendingVerbIntents(ctx, verb)
		if err != nil {
			return nil, err
		}
		for _, p := range pending {
			url := plan.TicketURL(p.TicketID)
			byTicket[url] = append(byTicket[url], plan.Intent{ID: p.ID, Ticket: url, Verb: verb, Payload: p.Payload})
		}
	}
	for _, intents := range byTicket {
		slices.SortFunc(intents, func(a, b plan.Intent) int { return cmp.Compare(a.ID, b.ID) })
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
	Step     plan.StepName
	Model    string
	Outcome  string
	Cost     option.Option[float64]
	Metrics  option.Option[agentlog.RunMetrics]
}

func (s *Store) SessionRunsForTicket(ctx context.Context, ticketID string) ([]SessionRun, error) {
	rows, err := s.q.LatestSessionRuns(ctx, notNull(ticketID))
	if err != nil {
		return nil, fmt.Errorf("select session runs for %s: %w", ticketID, err)
	}
	runs := make([]SessionRun, len(rows))
	for i, row := range rows {
		run := SessionRun{
			ID: row.ID, LogPath: row.LogPath, Message: row.Message, Question: row.Question,
			Step: plan.StepName(row.Step), Model: row.Model, Outcome: row.Outcome,
			Cost: option.None[float64](), Metrics: option.None[agentlog.RunMetrics](),
		}
		if row.CostUsd.Valid {
			run.Cost = option.Some(row.CostUsd.Float64)
		}
		if string(row.Metrics) != "null" {
			var m agentlog.RunMetrics
			if err := json.Unmarshal(row.Metrics, &m); err != nil {
				return nil, fmt.Errorf("decode metrics of run %d: %w", row.ID, err)
			}
			run.Metrics = option.Some(m)
		}
		runs[i] = run
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
