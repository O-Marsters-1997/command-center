package view_test

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestReader_Session(t *testing.T) {
	t.Parallel()

	t.Run("builds each run's log once", func(t *testing.T) {
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

		renders := 0
		count := func(view.LogLine) (template.HTML, error) {
			renders++
			return "", nil
		}
		want := 0
		for _, path := range logPaths {
			before := renders
			if _, err := view.BuildLogDetail(count, view.LogSource{Path: path}, view.Params{Log: view.LogAll}); err != nil {
				t.Fatalf("BuildLogDetail(%q): %v", path, err)
			}
			want += renders - before
		}
		renders = 0

		reader := view.NewReader(st, "", count, 0, 0)
		if _, err := reader.Session(ctx, testNow, view.SessionRef{Owner: "acme", Name: "web", N: "1"}, false); err != nil {
			t.Fatalf("Session(acme/web/1): %v", err)
		}
		if renders != want {
			t.Errorf("Session(acme/web/1) rendered %d lines, want %d (each run's log built once)", renders, want)
		}
	})
}
