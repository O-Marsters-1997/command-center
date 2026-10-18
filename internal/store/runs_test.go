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
	runID, err := store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
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
	if _, err := store.InsertCutFailedRun(t.Context(), "sandbox://CC-1", "hash-1", testNow); err != nil {
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
		if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
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
		if err := store.ConsumeVerbIntent(t.Context(), intent.ID, testNow.Add(time.Second)); err != nil {
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
		if err := store.ConsumeVerbIntent(t.Context(), intent.ID, testNow); err != nil {
			t.Fatalf("first ConsumeVerbIntent: %v", err)
		}
		if err := store.ConsumeVerbIntent(t.Context(), intent.ID, testNow); !errors.Is(err, storepkg.ErrNotFound) {
			t.Errorf("second ConsumeVerbIntent(%d) error = %v, want ErrNotFound", intent.ID, err)
		}
	})
}

func TestStore_QueueVerbIntent(t *testing.T) {
	t.Parallel()
	t.Run("a payload round-trips", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		prompt := "line one\nline \"two\""
		if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
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
		if err := store.QueueVerbIntent(t.Context(), storepkg.VerbRequest{
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

func TestStore_ActiveLaunchHashes(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	seedOneTicket(t, store)
	queueLaunch(t, store, storepkg.LaunchRequest{
		Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: testNow,
	})
	applyLaunches(t, store, testNow.Add(time.Second))

	hashes, err := store.ActiveLaunchHashes(t.Context())
	if err != nil {
		t.Fatalf("ActiveLaunchHashes: %v", err)
	}
	if diff := cmp.Diff(map[string]string{"sandbox://CC-1": "hash-1"}, hashes); diff != "" {
		t.Errorf("ActiveLaunchHashes() mismatch (-want +got):\n%s", diff)
	}
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
		if err := store.QueueVerbIntent(ctx, storepkg.VerbRequest{
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
	if err := store.ConsumeVerbIntent(ctx, kills[0].ID, testNow.Add(time.Minute)); err != nil {
		t.Fatalf("ConsumeVerbIntent(%d): %v", kills[0].ID, err)
	}
	want["sandbox://CC-1"] = want["sandbox://CC-1"][1:]
	if diff := cmp.Diff(want, read(t), ignoreID); diff != "" {
		t.Errorf("PendingIntents() after consuming the kill mismatch (-want +got):\n%s", diff)
	}
}

func TestStore_LatestSessionID(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)
	const ticket = "sandbox://CC-1"

	if _, err := store.LatestSessionID(ctx, ticket); !errors.Is(err, storepkg.ErrNotFound) {
		t.Fatalf("LatestSessionID(no runs) error = %v, want ErrNotFound", err)
	}
	_, agentSession, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: ticket, Kind: "agent", PromptHash: "hash", Session: storepkg.RunSession{StartNew: true},
	})
	if err != nil {
		t.Fatalf("InsertSessionRunSkeleton(agent): %v", err)
	}
	if agentSession == "" {
		t.Fatal("InsertSessionRunSkeleton(StartNew) session = \"\", want an id")
	}
	if _, err := store.InsertCutFailedRun(ctx, ticket, "", testNow); err != nil {
		t.Fatalf("InsertCutFailedRun: %v", err)
	}
	if got, err := store.LatestSessionID(ctx, ticket); err != nil || got != agentSession {
		t.Errorf("LatestSessionID() after an agent run and a cut failure = %q, %v, want %q", got, err, agentSession)
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
		if diff := cmp.Diff(want, got); diff != "" {
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
