package store_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/cctest"
)

func readRunCost(t *testing.T, dsn string, runID int64) sql.NullFloat64 {
	t.Helper()
	var cost sql.NullFloat64
	if err := openRawDB(t, dsn).QueryRow(`SELECT cost_usd FROM runs WHERE id = $1`, runID).Scan(&cost); err != nil {
		t.Fatalf("read cost for run %d: %v", runID, err)
	}
	return cost
}

func seedOneTicket(t *testing.T, store *storepkg.Store) {
	t.Helper()
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
}

func insertAgentRun(t *testing.T, store *storepkg.Store) int64 {
	t.Helper()
	runID, _, err := store.InsertSessionRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: "sandbox://CC-1", Kind: plan.RunKindAgent, BaselineSHA: "deadbeef", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton: %v", err)
	}
	return runID
}

func recordSpawn(t *testing.T, store *storepkg.Store, runID int64) {
	t.Helper()
	if err := store.RecordSpawn(t.Context(), storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: testNow, LogPath: "/state/runs/1.jsonl",
	}); err != nil {
		t.Fatalf("RecordSpawn: %v", err)
	}
}

func pendingRuns(t *testing.T, store *storepkg.Store) []storepkg.PendingRun {
	t.Helper()
	pending, err := store.PendingRunsAwaitingDisposition(t.Context())
	if err != nil {
		t.Fatalf("PendingRunsAwaitingDisposition: %v", err)
	}
	return pending
}

func latestRun(t *testing.T, store *storepkg.Store, ticket plan.TicketURL) plan.RunSummary {
	t.Helper()
	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatalf("LatestRunsByTicket: %v", err)
	}
	summary, ok := latest[ticket]
	if !ok {
		t.Fatalf("LatestRunsByTicket() has no entry for %s", ticket)
	}
	return summary
}

func TestStore_RecordSpawn(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	seedOneTicket(t, store)
	runID := insertAgentRun(t, store)

	if got := pendingRuns(t, store); len(got) != 0 {
		t.Errorf("PendingRunsAwaitingDisposition() before RecordSpawn = %+v, want none", got)
	}
	recordSpawn(t, store, runID)

	pending := pendingRuns(t, store)
	if len(pending) != 1 {
		t.Fatalf("PendingRunsAwaitingDisposition() = %+v, want one run", pending)
	}
	got := pending[0]
	if got.TicketID != "sandbox://CC-1" || got.Pgid != 4242 || got.BaselineSHA != "deadbeef" ||
		!got.ProcStartedAt.Equal(testNow) {
		t.Errorf("PendingRunsAwaitingDisposition()[0] = %+v, want CC-1 pgid 4242 baseline deadbeef started %s", got, testNow)
	}
	if summary := latestRun(t, store, "sandbox://CC-1"); summary.LogPath != "/state/runs/1.jsonl" ||
		summary.Outcome.IsSome() {
		t.Errorf("LatestRunsByTicket()[CC-1] = %+v, want the log path set and no outcome yet", summary)
	}
}

func TestStore_RecordDisposition(t *testing.T) {
	t.Parallel()

	t.Run("marks the run disposed with its exit and end", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		seedOneTicket(t, store)
		runID := insertAgentRun(t, store)
		recordSpawn(t, store, runID)
		endedAt := testNow.Add(30 * time.Second)
		if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
			RunID: plan.RunID(runID), Outcome: plan.OutcomeFailed, ExitCode: option.Some(0),
			Cost: option.None[float64](), EndedAt: endedAt,
		}); err != nil {
			t.Fatalf("RecordDisposition: %v", err)
		}

		if got := pendingRuns(t, store); len(got) != 0 {
			t.Errorf("PendingRunsAwaitingDisposition() = %+v, want none once disposed", got)
		}
		summary := latestRun(t, store, "sandbox://CC-1")
		if summary.Outcome != option.Some(plan.OutcomeFailed) || summary.ExitCode != option.Some(0) {
			t.Errorf("LatestRunsByTicket()[CC-1] = %+v, want outcome failed, exit 0", summary)
		}
		if summary.EndedAt.IsNone() || !summary.EndedAt.Unwrap().Equal(endedAt) {
			t.Errorf("LatestRunsByTicket()[CC-1].EndedAt = %v, want %s", summary.EndedAt, endedAt)
		}
	})

	t.Run("cost", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name string
			cost option.Option[float64]
			want sql.NullFloat64
		}{
			{name: "written alongside the outcome", cost: option.Some(0.42), want: sql.NullFloat64{Float64: 0.42, Valid: true}},
			{name: "NULL without a parse", cost: option.None[float64]()},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				dsn := cctest.DSN(t)
				store := openStoreAt(t, dsn)
				seedOneTicket(t, store)
				runID := insertAgentRun(t, store)
				if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
					RunID: plan.RunID(runID), Outcome: plan.OutcomePush, ExitCode: option.None[int](),
					Cost: tt.cost, EndedAt: testNow,
				}); err != nil {
					t.Fatalf("RecordDisposition: %v", err)
				}
				if got := readRunCost(t, dsn, runID); got != tt.want {
					t.Errorf("runs.cost_usd = %+v, want %+v", got, tt.want)
				}
			})
		}
	})

	t.Run("metrics", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name    string
			metrics option.Option[agentlog.RunMetrics]
			want    map[string]any
		}{
			{
				name: "written alongside the outcome without cost or requests",
				metrics: option.Some(agentlog.RunMetrics{
					CostUSD: option.Some(1.5), Requests: []agentlog.RequestUsage{{Output: 9}},
					Wall: option.Some(2 * time.Second), Turns: 3, Input: 1, CacheWrite: 2, CacheRead: 3, Output: 4,
					Tools: map[string]int{"Bash": 2},
				}),
				want: map[string]any{
					"wall": float64(2 * time.Second), "turns": 3.0, "input": 1.0, "cache_write": 2.0,
					"cache_read": 3.0, "output": 4.0, "tools": map[string]any{"Bash": 2.0},
				},
			},
			{name: "NULL when never measured", metrics: option.None[agentlog.RunMetrics]()},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				dsn := cctest.DSN(t)
				store := openStoreAt(t, dsn)
				seedOneTicket(t, store)
				runID := insertAgentRun(t, store)
				if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
					RunID: plan.RunID(runID), Outcome: plan.OutcomePush, Metrics: tt.metrics, EndedAt: testNow,
				}); err != nil {
					t.Fatalf("RecordDisposition: %v", err)
				}
				var raw sql.NullString
				if err := openRawDB(t, dsn).QueryRow(`SELECT metrics::text FROM runs WHERE id = $1`, runID).Scan(&raw); err != nil {
					t.Fatalf("read metrics for run %d: %v", runID, err)
				}
				var got map[string]any
				if raw.Valid {
					if err := json.Unmarshal([]byte(raw.String), &got); err != nil {
						t.Fatalf("decode runs.metrics %q: %v", raw.String, err)
					}
				}
				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("runs.metrics mismatch (-want +got):\n%s", diff)
				}
			})
		}
	})

	t.Run("twice reports not found", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		seedOneTicket(t, store)
		disposition := storepkg.Disposition{
			RunID: plan.RunID(insertAgentRun(t, store)), Outcome: plan.OutcomePush, EndedAt: testNow,
		}
		if err := store.RecordDisposition(t.Context(), disposition); err != nil {
			t.Fatalf("first RecordDisposition: %v", err)
		}
		if err := store.RecordDisposition(t.Context(), disposition); !errors.Is(err, storepkg.ErrNotFound) {
			t.Errorf("second RecordDisposition() error = %v, want ErrNotFound", err)
		}
	})
}

func TestStore_InsertCutFailedRun(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	seedOneTicket(t, store)
	cut := storepkg.RunSkeleton{Ticket: "sandbox://CC-1", PromptHash: "hash-1"}
	if _, err := store.InsertCutFailedRun(t.Context(), cut, testNow); err != nil {
		t.Fatalf("InsertCutFailedRun: %v", err)
	}

	if got := pendingRuns(t, store); len(got) != 0 {
		t.Errorf("PendingRunsAwaitingDisposition() = %+v, want none: a cut failure never claims a pgid", got)
	}
	if summary := latestRun(t, store, "sandbox://CC-1"); summary.Outcome != option.Some(plan.OutcomeCutFailed) ||
		summary.Pgid.IsSome() {
		t.Errorf("LatestRunsByTicket()[CC-1] = %+v, want cut_failed with no pgid", summary)
	}
}

func TestStore_ConsumeVerbIntent(t *testing.T) {
	t.Parallel()
	queueKill := func(t *testing.T, store *storepkg.Store) storepkg.VerbIntent {
		t.Helper()
		if _, err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: "sandbox://CC-1", Verb: plan.VerbKill, At: testNow,
		}); err != nil {
			t.Fatalf("QueueVerbIntent: %v", err)
		}
		pending, err := store.PendingVerbIntents(t.Context(), plan.VerbKill)
		if err != nil {
			t.Fatalf("PendingVerbIntents(kill): %v", err)
		}
		if len(pending) != 1 || pending[0].TicketID != "sandbox://CC-1" {
			t.Fatalf("PendingVerbIntents(kill) = %+v, want one kill intent for CC-1", pending)
		}
		return pending[0]
	}

	t.Run("a consumed intent is no longer pending", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		seedOneTicket(t, store)
		intent := queueKill(t, store)
		consumption := storepkg.Consumption{At: testNow.Add(time.Second)}
		if err := store.ConsumeVerbIntent(t.Context(), intent.ID, consumption); err != nil {
			t.Fatalf("ConsumeVerbIntent: %v", err)
		}
		after, err := store.PendingVerbIntents(t.Context(), plan.VerbKill)
		if err != nil {
			t.Fatalf("PendingVerbIntents(kill): %v", err)
		}
		if len(after) != 0 {
			t.Errorf("PendingVerbIntents(kill) after consume = %+v, want none", after)
		}
	})

	t.Run("twice reports not found", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		intent := queueKill(t, store)
		consumption := storepkg.Consumption{At: testNow}
		if err := store.ConsumeVerbIntent(t.Context(), intent.ID, consumption); err != nil {
			t.Fatalf("first ConsumeVerbIntent: %v", err)
		}
		if err := store.ConsumeVerbIntent(t.Context(), intent.ID, consumption); !errors.Is(err, storepkg.ErrNotFound) {
			t.Errorf("second ConsumeVerbIntent(%d) error = %v, want ErrNotFound", intent.ID, err)
		}
	})
}

func TestStore_WithdrawHeldIntent(t *testing.T) {
	t.Parallel()
	queueHeld := func(t *testing.T, store *storepkg.Store) int64 {
		t.Helper()
		id, err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: "sandbox://CC-1", Verb: plan.VerbKill, At: testNow, HoldUntil: testNow.Add(5 * time.Second),
		})
		if err != nil {
			t.Fatalf("QueueVerbIntent: %v", err)
		}
		return id
	}

	t.Run("within the hold the intent is deleted", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		id := queueHeld(t, store)
		if err := store.WithdrawHeldIntent(t.Context(), id, testNow.Add(time.Second)); err != nil {
			t.Fatalf("WithdrawHeldIntent: %v", err)
		}
		pending, err := store.PendingVerbIntents(t.Context(), plan.VerbKill)
		if err != nil || len(pending) != 0 {
			t.Errorf("PendingVerbIntents(kill) = %+v, %v, want none", pending, err)
		}
	})

	t.Run("past the hold, or once consumed, it is too late", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		id := queueHeld(t, store)
		err := store.WithdrawHeldIntent(t.Context(), id, testNow.Add(6*time.Second))
		if !errors.Is(err, storepkg.ErrIntentTooLate) {
			t.Errorf("WithdrawHeldIntent past hold error = %v, want ErrIntentTooLate", err)
		}
		consumption := storepkg.Consumption{At: testNow.Add(6 * time.Second)}
		if err := store.ConsumeVerbIntent(t.Context(), id, consumption); err != nil {
			t.Fatalf("ConsumeVerbIntent: %v", err)
		}
		err = store.WithdrawHeldIntent(t.Context(), id, testNow.Add(time.Second))
		if !errors.Is(err, storepkg.ErrIntentTooLate) {
			t.Errorf("WithdrawHeldIntent after consume error = %v, want ErrIntentTooLate", err)
		}
	})
}

func TestStore_RecordDisposition_closesSpawnIntent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		outcome plan.Outcome
		want    plan.IntentOutcome
	}{
		{plan.OutcomePush, plan.IntentDone},
		{plan.OutcomeFailed, plan.IntentFailed},
	} {
		t.Run(string(tc.want), func(t *testing.T) {
			t.Parallel()
			testRecordDispositionClosesSpawnIntent(t, tc.outcome, tc.want)
		})
	}
}

func testRecordDispositionClosesSpawnIntent(t *testing.T, outcome plan.Outcome, want plan.IntentOutcome) {
	t.Helper()
	store := openStore(t)
	seedOneTicket(t, store)
	ctx := t.Context()
	runID := insertAgentRun(t, store)
	recordSpawn(t, store, runID)
	req := storepkg.VerbRequest{Ticket: "sandbox://CC-1", Verb: plan.VerbReRun, At: testNow}
	id, err := store.QueueVerbIntent(ctx, req)
	if err != nil {
		t.Fatalf("QueueVerbIntent: %v", err)
	}
	consumption := storepkg.Consumption{At: testNow, Run: option.Some(plan.RunID(runID))}
	if err := store.ConsumeVerbIntent(ctx, id, consumption); err != nil {
		t.Fatalf("ConsumeVerbIntent: %v", err)
	}
	ended := testNow.Add(time.Minute)
	if err := store.RecordDisposition(ctx, storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: outcome, EndedAt: ended,
	}); err != nil {
		t.Fatalf("RecordDisposition: %v", err)
	}

	records, err := store.IntentHistory(ctx, testNow)
	if err != nil {
		t.Fatalf("IntentHistory: %v", err)
	}
	if len(records) != 1 || records[0].Outcome != want || !records[0].FinishedAt.Unwrap().Equal(ended) {
		t.Errorf("IntentHistory = %+v, want one intent %v at %v", records, want, ended)
	}
}

func TestStore_IntentHistory(t *testing.T) {
	t.Parallel()
	const ticket = "sandbox://CC-1"
	queue := func(t *testing.T, store *storepkg.Store, req storepkg.VerbRequest) int64 {
		t.Helper()
		req.Ticket, req.At = ticket, testNow
		id, err := store.QueueVerbIntent(t.Context(), req)
		if err != nil {
			t.Fatalf("QueueVerbIntent(%s): %v", req.Verb, err)
		}
		return id
	}
	history := func(t *testing.T, store *storepkg.Store, since time.Time) map[int64]plan.IntentRecord {
		t.Helper()
		records, err := store.IntentHistory(t.Context(), since)
		if err != nil {
			t.Fatalf("IntentHistory: %v", err)
		}
		byID := map[int64]plan.IntentRecord{}
		for _, r := range records {
			byID[r.ID] = r
		}
		return byID
	}
	at := testNow.Add(time.Second)

	t.Run("a refusal and a success end with their outcome and detail", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		refused := queue(t, store, storepkg.VerbRequest{Verb: plan.VerbResolve})
		aborted := queue(t, store, storepkg.VerbRequest{Verb: plan.VerbAbort})
		consume := func(id int64, c storepkg.Consumption) {
			t.Helper()
			if err := store.ConsumeVerbIntent(t.Context(), id, c); err != nil {
				t.Fatalf("ConsumeVerbIntent(%d): %v", id, err)
			}
		}
		consume(refused, storepkg.Consumption{
			At: at, Outcome: plan.IntentFailed, Detail: "no conflict is recorded", TicketState: plan.NeedsYou,
		})
		consume(aborted, storepkg.Consumption{At: at, Outcome: plan.IntentDone, TicketState: plan.NeedsYou})

		got := history(t, store, testNow)
		rec := got[refused]
		if rec.Outcome != plan.IntentFailed || rec.Detail != "no conflict is recorded" ||
			rec.TicketState != plan.NeedsYou || rec.QueuedBy != plan.QueuedByOwner ||
			!rec.FinishedAt.UnwrapOr(time.Time{}).Equal(at) {
			t.Errorf("refused resolve record = %+v, want failed with its refusal, state and finish time", rec)
		}
		if got[aborted].Outcome != plan.IntentDone || got[aborted].FinishedAt.IsNone() {
			t.Errorf("abort record = %+v, want done and finished", got[aborted])
		}
	})

	t.Run("an agent verb is consumed with its run and no outcome", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		seedOneTicket(t, store)
		id := queue(t, store, storepkg.VerbRequest{Verb: plan.VerbReRun})
		runID, _, err := store.InsertSessionRunSkeleton(
			t.Context(), storepkg.RunSkeleton{Ticket: ticket, Kind: plan.RunKindAgent})
		if err != nil {
			t.Fatalf("InsertSessionRunSkeleton: %v", err)
		}
		c := storepkg.Consumption{At: at, Run: option.Some(plan.RunID(runID))}
		if err := store.ConsumeVerbIntent(t.Context(), id, c); err != nil {
			t.Fatalf("ConsumeVerbIntent: %v", err)
		}
		rec := history(t, store, testNow.Add(time.Hour))[id]
		if rec.Run != option.Some(plan.RunID(runID)) || rec.Outcome != plan.IntentInProgress || rec.FinishedAt.IsSome() {
			t.Errorf("re-run record = %+v, want run %d, no outcome, unfinished", rec, runID)
		}
	})

	t.Run("undo inside the hold leaves a withdrawn row", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		id := queue(t, store, storepkg.VerbRequest{Verb: plan.VerbKill, HoldUntil: testNow.Add(5 * time.Second)})
		if err := store.WithdrawHeldIntent(t.Context(), id, at); err != nil {
			t.Fatalf("WithdrawHeldIntent: %v", err)
		}
		rec := history(t, store, testNow)[id]
		if rec.Outcome != plan.IntentWithdrawn || rec.ConsumedAt.IsNone() || rec.FinishedAt.IsNone() {
			t.Errorf("withdrawn record = %+v, want withdrawn, consumed and finished", rec)
		}
	})

	t.Run("a loop-queued removal is stored as the loop's", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		id := queue(t, store, storepkg.VerbRequest{Verb: plan.VerbRemoveWorktree, QueuedBy: plan.QueuedByLoop})
		if got := history(t, store, testNow)[id].QueuedBy; got != plan.QueuedByLoop {
			t.Errorf("QueuedBy = %q, want %q", got, plan.QueuedByLoop)
		}
	})

	t.Run("it leaves out column verbs and finished rows older than since", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		tracked := queue(t, store, storepkg.VerbRequest{Verb: plan.VerbTrack})
		old := queue(t, store, storepkg.VerbRequest{Verb: plan.VerbAbort})
		finished := storepkg.Consumption{At: at, Outcome: plan.IntentDone}
		if err := store.ConsumeVerbIntent(t.Context(), old, finished); err != nil {
			t.Fatalf("ConsumeVerbIntent: %v", err)
		}
		pending := queue(t, store, storepkg.VerbRequest{Verb: plan.VerbFollowUp})
		got := history(t, store, testNow.Add(time.Hour))
		if _, ok := got[tracked]; ok {
			t.Errorf("history includes the track intent %d, want only row verbs", tracked)
		}
		if _, ok := got[old]; ok {
			t.Errorf("history includes finished intent %d queued before since", old)
		}
		if _, ok := got[pending]; !ok {
			t.Errorf("history omits pending intent %d, want in-progress rows always", pending)
		}
	})
}

func TestStore_LatestRowIntents(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	queue := func(ticket string, verb plan.Verb) int64 {
		t.Helper()
		id, err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{Ticket: ticket, Verb: verb, At: testNow})
		if err != nil {
			t.Fatalf("QueueVerbIntent(%s): %v", verb, err)
		}
		return id
	}
	queue("sandbox://CC-1", plan.VerbAbort)
	latest := queue("sandbox://CC-1", plan.VerbResolve)
	queue("sandbox://CC-1", plan.VerbTrack)
	other := queue("sandbox://CC-2", plan.VerbKill)

	got, err := store.LatestRowIntents(t.Context())
	if err != nil {
		t.Fatalf("LatestRowIntents: %v", err)
	}
	if len(got) != 2 || got["sandbox://CC-1"].ID != latest || got["sandbox://CC-2"].ID != other {
		t.Errorf("LatestRowIntents() = %+v, want the newest row verb per ticket (%d and %d)", got, latest, other)
	}
}

func TestStore_QueueVerbIntent(t *testing.T) {
	t.Parallel()
	t.Run("a payload round-trips", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		prompt := "line one\nline \"two\""
		if _, err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: "https://x/1", Verb: plan.VerbFollowUp, Payload: prompt, At: testNow,
		}); err != nil {
			t.Fatalf("QueueVerbIntent: %v", err)
		}
		got, err := store.PendingVerbIntents(t.Context(), plan.VerbFollowUp)
		if err != nil {
			t.Fatalf("PendingVerbIntents(follow_up): %v", err)
		}
		if len(got) != 1 || got[0].Payload != prompt {
			t.Errorf("PendingVerbIntents(follow_up) = %+v, want one with payload %q", got, prompt)
		}
	})

	t.Run("an empty payload is stored as NULL", func(t *testing.T) {
		t.Parallel()
		dsn := cctest.DSN(t)
		store := openStoreAt(t, dsn)
		if _, err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
			Ticket: "https://x/1", Verb: plan.VerbKill, At: testNow,
		}); err != nil {
			t.Fatalf("QueueVerbIntent: %v", err)
		}
		var isNull bool
		row := openRawDB(t, dsn).QueryRow(`SELECT payload IS NULL FROM intents WHERE ticket_id = $1`, "https://x/1")
		if err := row.Scan(&isNull); err != nil {
			t.Fatalf("read intent payload: %v", err)
		}
		if !isNull {
			t.Error("intents.payload is not NULL for an intent queued without a payload")
		}
	})
}

func TestStore_PendingIntents(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)
	second := []storepkg.Ticket{{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second"}}
	if err := store.UpsertTickets(ctx, second); err != nil {
		t.Fatalf("UpsertTickets(CC-2): %v", err)
	}
	for i, q := range []struct {
		ticket  string
		verb    plan.Verb
		payload string
	}{
		{"sandbox://CC-1", plan.VerbKill, ""},
		{"sandbox://CC-1", plan.VerbFollowUp, "go on"},
		{"sandbox://CC-2", plan.VerbReRun, ""},
	} {
		if _, err := store.QueueVerbIntent(ctx, storepkg.VerbRequest{
			Ticket: q.ticket, Verb: q.verb, Payload: q.payload, At: testNow.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("QueueVerbIntent(%s, %s): %v", q.ticket, q.verb, err)
		}
	}
	read := func(t *testing.T) map[plan.TicketURL][]plan.Intent {
		t.Helper()
		byTicket, err := store.PendingIntents(ctx)
		if err != nil {
			t.Fatalf("PendingIntents: %v", err)
		}
		return byTicket
	}
	ignoreID := cmpopts.IgnoreFields(plan.Intent{}, "ID")

	want := map[plan.TicketURL][]plan.Intent{
		"sandbox://CC-1": {
			{Ticket: "sandbox://CC-1", Verb: plan.VerbKill},
			{Ticket: "sandbox://CC-1", Verb: plan.VerbFollowUp, Payload: "go on"},
		},
		"sandbox://CC-2": {{Ticket: "sandbox://CC-2", Verb: plan.VerbReRun}},
	}
	if diff := cmp.Diff(want, read(t), ignoreID); diff != "" {
		t.Fatalf("PendingIntents() mismatch (-want +got):\n%s", diff)
	}

	kills, err := store.PendingVerbIntents(ctx, plan.VerbKill)
	if err != nil {
		t.Fatalf("PendingVerbIntents(kill): %v", err)
	}
	if err := store.ConsumeVerbIntent(ctx, kills[0].ID, storepkg.Consumption{At: testNow.Add(time.Minute)}); err != nil {
		t.Fatalf("ConsumeVerbIntent(%d): %v", kills[0].ID, err)
	}
	want["sandbox://CC-1"] = want["sandbox://CC-1"][1:]
	if diff := cmp.Diff(want, read(t), ignoreID); diff != "" {
		t.Errorf("PendingIntents() after consuming the kill mismatch (-want +got):\n%s", diff)
	}
}

func TestStore_InsertSessionRunSkeleton(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)
	const ticket = "sandbox://CC-1"

	_, agentSession, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: ticket, Kind: "agent", PromptHash: "hash", Session: storepkg.RunSession{StartNew: true},
	})
	if err != nil {
		t.Fatalf("InsertSessionRunSkeleton(agent): %v", err)
	}
	if agentSession == "" {
		t.Fatal("InsertSessionRunSkeleton(StartNew) session = \"\", want an id")
	}
	_, followSession, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: ticket, Kind: "follow_up", Session: storepkg.RunSession{JoinID: agentSession, Message: "go on"},
	})
	if err != nil {
		t.Fatalf("InsertSessionRunSkeleton(follow-up): %v", err)
	}
	if followSession != agentSession {
		t.Errorf("InsertSessionRunSkeleton(join) session = %q, want %q", followSession, agentSession)
	}
}

func TestStore_AutoHistories(t *testing.T) {
	t.Parallel()
	const url = plan.TicketURL("sandbox://CC-1")
	setup := func(t *testing.T) *storepkg.Store {
		t.Helper()
		store := openStore(t)
		seedOneTicket(t, store)
		return store
	}
	followUp := func(t *testing.T, store *storepkg.Store, tip string, cause plan.FollowUpCause) {
		t.Helper()
		runID, _, err := store.InsertSessionRunSkeleton(t.Context(), storepkg.RunSkeleton{
			Ticket: string(url), Kind: plan.RunKindFollowUp, BaselineSHA: tip, PromptHash: "h", Cause: cause,
		})
		if err != nil {
			t.Fatalf("InsertRunSkeleton: %v", err)
		}
		recordSpawn(t, store, runID)
	}

	t.Run("counts automatic rounds and reports the latest tip, ignoring operator follow-ups", func(t *testing.T) {
		t.Parallel()
		store := setup(t)
		followUp(t, store, "tip-1", plan.CauseCIFailed)
		followUp(t, store, "tip-2", plan.CauseNone)
		followUp(t, store, "tip-3", plan.CauseCIFailed)
		in, err := store.PlanInput(t.Context())
		if err != nil {
			t.Fatalf("PlanInput: %v", err)
		}
		want := map[plan.TicketURL]plan.AutoHistory{url: {Rounds: 2, LastTip: "tip-3"}}
		if diff := cmp.Diff(want, in.Auto); diff != "" {
			t.Errorf("PlanInput().Auto mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a green verdict transition resets the rounds", func(t *testing.T) {
		t.Parallel()
		store := setup(t)
		followUp(t, store, "tip-1", plan.CauseCIFailed)
		if err := store.AppendEvent(t.Context(), storepkg.Event{
			At: testNow.Add(time.Minute), TicketURL: string(url), Kind: storepkg.EventVerdictTransition,
			Detail: "review_me: every required check passed",
		}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
		in, err := store.PlanInput(t.Context())
		if err != nil {
			t.Fatalf("PlanInput: %v", err)
		}
		if len(in.Auto) != 0 {
			t.Errorf("PlanInput().Auto after green = %+v, want none", in.Auto)
		}
	})
}

func TestStore_SessionRunsForTicket(t *testing.T) {
	t.Parallel()
	const ticket = "sandbox://CC-1"

	t.Run("the latest session in order", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		seedOneTicket(t, store)
		if got, err := store.SessionRunsForTicket(ctx, ticket); err != nil || len(got) != 0 {
			t.Fatalf("SessionRunsForTicket(no runs) = %v, %v, want none", got, err)
		}
		_, oldSession, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
			Ticket: ticket, Kind: "agent", PromptHash: "hash", Session: storepkg.RunSession{StartNew: true},
		})
		if err != nil {
			t.Fatalf("InsertSessionRunSkeleton(old session): %v", err)
		}
		launchID, session, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
			Ticket: ticket, Kind: "agent", PromptHash: "hash", Session: storepkg.RunSession{StartNew: true},
		})
		if err != nil {
			t.Fatalf("InsertSessionRunSkeleton(new session): %v", err)
		}
		if session == oldSession {
			t.Fatalf("InsertSessionRunSkeleton(StartNew) reused session %q", session)
		}
		followID, _, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
			Ticket: ticket, Kind: "follow_up", Session: storepkg.RunSession{JoinID: session, Message: "go on"},
		})
		if err != nil {
			t.Fatalf("InsertSessionRunSkeleton(follow-up): %v", err)
		}

		got, err := store.SessionRunsForTicket(ctx, ticket)
		if err != nil {
			t.Fatalf("SessionRunsForTicket: %v", err)
		}
		want := []storepkg.SessionRun{{ID: launchID}, {ID: followID, Message: "go on"}}
		if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(storepkg.SessionRun{}, "Cost", "Metrics")); diff != "" {
			t.Errorf("SessionRunsForTicket(%s) mismatch (-want +got):\n%s", ticket, diff)
		}
	})

	t.Run("empty when the latest run has no session", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		seedOneTicket(t, store)
		insertAgentRun(t, store)
		if got, err := store.SessionRunsForTicket(t.Context(), ticket); err != nil || len(got) != 0 {
			t.Errorf("SessionRunsForTicket(no session) = %v, %v, want none", got, err)
		}
	})
}
