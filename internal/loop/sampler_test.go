package loop_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/O-Marsters-1997/command-center/internal/adapter/meter"
	"github.com/O-Marsters-1997/command-center/internal/adapter/meter/metertest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/cctest"
)

type sampleFixture struct {
	store *storepkg.Store
	db    *sql.DB
	at    time.Time
}

func newSampleFixture(t *testing.T) *sampleFixture {
	t.Helper()
	dsn := cctest.DSN(t)
	st, err := storepkg.OpenStore(dsn, storepkg.MaxOpenConns(4), storepkg.SkipMigrate())
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open(pgx): %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close raw database: %v", err)
		}
	})
	return &sampleFixture{store: st, db: db, at: time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)}
}

func (f *sampleFixture) liveRun(t *testing.T, pgid int) int64 {
	t.Helper()
	ctx := t.Context()
	tickets := []storepkg.Ticket{{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}}
	if err := f.store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	id, _, err := f.store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: "sandbox://CC-1", Kind: plan.RunKindAgent, Step: "implement", Workflow: "v0", WorkflowHash: "h",
	})
	if err != nil {
		t.Fatalf("InsertSessionRunSkeleton: %v", err)
	}
	if err := f.store.RecordSpawn(ctx, storepkg.Spawn{RunID: plan.RunID(id), Pgid: pgid, StartedAt: f.at}); err != nil {
		t.Fatalf("RecordSpawn: %v", err)
	}
	return id
}

func (f *sampleFixture) count(t *testing.T, query string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(t.Context(), query).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

func (f *sampleFixture) sampler(m meter.Meter, clock loop.Clock) *loop.Sampler {
	return &loop.Sampler{Logger: slog.New(slog.DiscardHandler), Store: f.store, Meter: m, Clock: clock}
}

func TestSampler_SampleOnce(t *testing.T) {
	t.Parallel()

	t.Run("a live run's processes are recorded against it and others' are dropped", func(t *testing.T) {
		t.Parallel()
		f := newSampleFixture(t)
		runID := f.liveRun(t, 4242)
		m := metertest.Meter{Total: 32 << 30, Used: 8 << 30, Procs: []meter.Process{
			{Pgid: 4242, Pid: 4242, Name: "claude", Args: "claude --print", RSS: 500 << 20},
			{Pgid: 4242, Pid: 4243, Name: "gopls", Args: "gopls serve", RSS: 700 << 20},
			{Pgid: 9, Pid: 9, Name: "other", Args: "other", RSS: 1 << 20},
		}}

		if err := f.sampler(m, fixedClock(f.at)).SampleOnce(t.Context()); err != nil {
			t.Fatalf("SampleOnce() error = %v", err)
		}

		if got := f.count(t, "SELECT count(*) FROM memory_samples WHERE used_bytes = 8589934592"); got != 1 {
			t.Errorf("samples = %d, want 1", got)
		}
		query := "SELECT count(*) FROM memory_processes WHERE run_id = " + strconv.FormatInt(runID, 10)
		if got := f.count(t, query); got != 2 {
			t.Errorf("processes of run = %d, want 2", got)
		}
		if got := f.count(t, "SELECT count(*) FROM memory_processes"); got != 2 {
			t.Errorf("processes = %d, want 2", got)
		}
	})

	t.Run("no live run still records a sample without processes", func(t *testing.T) {
		t.Parallel()
		f := newSampleFixture(t)

		if err := f.sampler(metertest.Meter{Total: 1, Used: 0}, fixedClock(f.at)).SampleOnce(t.Context()); err != nil {
			t.Fatalf("SampleOnce() error = %v", err)
		}

		if got := f.count(t, "SELECT count(*) FROM memory_samples"); got != 1 {
			t.Errorf("samples = %d, want 1", got)
		}
		if got := f.count(t, "SELECT count(*) FROM memory_processes"); got != 0 {
			t.Errorf("processes = %d, want 0", got)
		}
	})
}

type flakyMeter struct{ calls int }

func (m *flakyMeter) Read(context.Context, []int) (meter.Reading, error) {
	m.calls++
	if m.calls == 1 {
		return meter.Reading{}, errors.New("ps unavailable")
	}
	return meter.Reading{Total: 1, Used: 1}, nil
}

type tickClock struct {
	at    time.Time
	ticks chan time.Time
}

func (c tickClock) Now() time.Time                       { return c.at }
func (c tickClock) After(time.Duration) <-chan time.Time { return c.ticks }

func TestSampler_Run(t *testing.T) {
	t.Parallel()
	f := newSampleFixture(t)
	clock := tickClock{at: f.at, ticks: make(chan time.Time)}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.sampler(&flakyMeter{}, clock).Run(ctx) }()

	clock.ticks <- f.at
	clock.ticks <- f.at
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := f.count(t, "SELECT count(*) FROM memory_samples"); got < 1 {
		t.Errorf("samples after a failed read then an interval = %d, want at least 1", got)
	}
}
