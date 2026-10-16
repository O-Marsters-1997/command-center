package view_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/cctest"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestReader_Session(t *testing.T) {
	t.Parallel()

	t.Run("gives each turn its own run's log", func(t *testing.T) {
		t.Parallel()
		const ticketURL = "https://github.com/acme/web/issues/1"
		ctx := t.Context()
		st := cctest.Store(t)
		storetest.Tickets(t, st, store.Ticket{URL: ticketURL, Repo: "acme/web", Branch: "cc-1"})

		dir := t.TempDir()
		var logPaths []string
		session := store.RunSession{StartNew: true}
		for i := range 3 {
			path := filepath.Join(dir, fmt.Sprintf("%d.jsonl", i))
			body := ""
			for j := range i + 2 {
				body += fmt.Sprintf(`{"type":"assistant","timestamp":"2026-08-20T12:00:0%dZ",`+
					`"message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"run %d step %d"}}]}}`+"\n",
					j, i, j)
			}
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("WriteFile(%q): %v", path, err)
			}
			logPaths = append(logPaths, path)

			runID, sessionID, err := st.InsertSessionRunSkeleton(ctx, store.RunSkeleton{
				Ticket: ticketURL, Kind: plan.RunKindAgent, PromptHash: "hash", Session: session,
			})
			if err != nil {
				t.Fatalf("InsertSessionRunSkeleton(%q): %v", ticketURL, err)
			}
			session = store.RunSession{JoinID: sessionID}
			if err := st.RecordSpawn(ctx, store.Spawn{
				RunID: plan.RunID(runID), Pgid: 99, StartedAt: testNow, LogPath: path,
			}); err != nil {
				t.Fatalf("RecordSpawn(%d): %v", runID, err)
			}
			if err := st.RecordDisposition(ctx, store.Disposition{
				RunID: plan.RunID(runID), Outcome: plan.OutcomePush, ExitCode: option.Some(0),
				Cost: option.None[float64](), EndedAt: testNow,
			}); err != nil {
				t.Fatalf("RecordDisposition(%d): %v", runID, err)
			}
		}
		obs := plan.Observation{ObservedAt: testNow, Runs: map[plan.TicketURL]plan.RunObservation{ticketURL: {}}}
		if err := st.SaveObservation(ctx, obs); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}

		reader := view.NewReader(st, "", 0, 0)
		page, err := reader.Session(ctx, testNow, view.SessionRef{Owner: "acme", Name: "web", N: "1"}, false)
		if err != nil {
			t.Fatalf("Session(acme/web/1): %v", err)
		}
		if len(page.Turns) != len(logPaths) {
			t.Fatalf("Session(acme/web/1) turns = %d, want %d", len(page.Turns), len(logPaths))
		}
		for i, turn := range page.Turns {
			if turn.Log.Path != logPaths[i] || turn.Log.Lines != i+2 {
				t.Errorf("Session(acme/web/1) turn %d log = {%q, %d lines}, want {%q, %d lines}",
					i, turn.Log.Path, turn.Log.Lines, logPaths[i], i+2)
			}
		}
	})
}
