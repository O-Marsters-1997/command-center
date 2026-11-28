package web_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/memory"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/cctest"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func TestActivityPage_memory(t *testing.T) {
	t.Parallel()
	const gib = int64(1) << 30
	spikeAt := testNow
	quietAt := testNow.Add(-5 * time.Hour)

	seeded := func(t *testing.T) *storepkg.Store {
		t.Helper()
		st := track(t, openStore(t), named("cc-sandbox")...)
		storetest.Tickets(t, st, storepkg.Ticket{URL: activityTicket, Repo: "cc-sandbox", Branch: "cc-1", Feature: "feat-a"})
		var procs []memory.ProcFact
		for pid := range 3 {
			run := storetest.Run(t, st, storetest.RunOpts{
				TicketURL: activityTicket, Kind: "agent", PromptHash: "h", Session: storepkg.RunSession{StartNew: true},
				StartedAt: testNow,
			})
			procs = append(procs, memory.ProcFact{RunID: run, Pid: pid + 1, Name: "gopls", RSS: gib})
		}
		for _, s := range []memory.Sample{
			{At: spikeAt, Total: 16 * gib, Used: 15 * gib, Procs: procs},
			{At: quietAt, Total: 16 * gib, Used: 4 * gib},
		} {
			if err := st.RecordMemorySample(t.Context(), s); err != nil {
				t.Fatalf("RecordMemorySample: %v", err)
			}
		}
		return st
	}
	mem := func(t *testing.T, st *storepkg.Store, path string) htmltest.Node {
		t.Helper()
		return htmltest.Parse(t, renderPath(t, newServer(st, testNow), path)).One(t, "#mem")
	}

	t.Run("the latest bucket is selected and the caption names the culprit", func(t *testing.T) {
		t.Parallel()
		section := mem(t, seeded(t), "/activity")

		if got := section.One(t, `a[aria-current="true"][title]`).Text(); got != "" {
			t.Errorf("selected bar text = %q, want an empty bar", got)
		}
		caption := section.One(t, "p[aria-live]").Text()
		for _, want := range []string{"15.0 GB", "gopls", "3 runs"} {
			if !strings.Contains(caption, want) {
				t.Errorf("GET /activity caption = %q, want %q", caption, want)
			}
		}
	})

	t.Run("the at bucket is selected and a bucket with no run processes says so", func(t *testing.T) {
		t.Parallel()
		path := fmt.Sprintf("/activity?from=%d&to=%d&at=%d",
			testNow.Add(-24*time.Hour).Unix(), testNow.Add(15*time.Minute).Unix(), quietAt.Unix())
		caption := mem(t, seeded(t), path).One(t, "p[aria-live]").Text()

		for _, want := range []string{"4.0 GB", "No agents running."} {
			if !strings.Contains(caption, want) {
				t.Errorf("GET %s caption = %q, want %q", path, caption, want)
			}
		}
	})

	t.Run("a window ending now polls and a past window does not", func(t *testing.T) {
		t.Parallel()
		st := seeded(t)
		live := mem(t, st, "/activity")
		if got, _ := live.Attr("hx-trigger"); got != "every 5s" {
			t.Errorf("live #mem hx-trigger = %q, want every 5s", got)
		}
		past := fmt.Sprintf("/activity?from=%d&to=%d", testNow.Add(-48*time.Hour).Unix(), testNow.Add(-24*time.Hour).Unix())
		if got, ok := mem(t, st, past).Attr("hx-trigger"); ok {
			t.Errorf("past #mem hx-trigger = %q, want none", got)
		}
	})

	t.Run("a store error renders in #mem and the page still loads", func(t *testing.T) {
		t.Parallel()
		dsn := cctest.DSN(t)
		st, err := storepkg.OpenStore(dsn, storepkg.SkipMigrate())
		if err != nil {
			t.Fatalf("OpenStore: %v", err)
		}
		t.Cleanup(func() {
			if err := st.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		})
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatalf("sql.Open: %v", err)
		}
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Errorf("close raw database: %v", err)
			}
		})
		if _, err := db.ExecContext(t.Context(), "DROP TABLE memory_processes, memory_samples"); err != nil {
			t.Fatalf("drop memory tables: %v", err)
		}

		page := renderPath(t, newServer(st, testNow), "/activity")
		parsed := htmltest.Parse(t, page)
		if got := parsed.One(t, "#mem [role=alert]").Text(); !strings.Contains(got, "Could not load memory") {
			t.Errorf("GET /activity #mem alert = %q, want the memory error", got)
		}
		parsed.One(t, "#activity-results")
	})
}
