package web_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func transcriptStore(t *testing.T, messages ...string) *storepkg.Store {
	t.Helper()
	ctx := t.Context()
	st := openStore(t)
	ticket := storepkg.Ticket{URL: sessionTicket, Repo: "acme/web", Branch: "cc-1", Feature: "checkout"}
	storetest.Tickets(t, st, ticket)
	dir := t.TempDir()
	session := storepkg.RunSession{StartNew: true}
	for i, message := range messages {
		kind := plan.RunKindFollowUp
		if i == 0 {
			kind = plan.RunKindAgent
		}
		session.Message = message
		runID, sessionID, err := st.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
			Ticket: sessionTicket, Kind: kind, PromptHash: "hash", Session: session,
		})
		if err != nil {
			t.Fatalf("InsertSessionRunSkeleton(%q): %v", sessionTicket, err)
		}
		session = storepkg.RunSession{JoinID: sessionID}

		logPath := filepath.Join(dir, fmt.Sprintf("%d.jsonl", i+1))
		line := fmt.Sprintf(`{"type":"assistant","timestamp":"2026-08-20T12:00:0%dZ",`+
			`"message":{"content":[{"type":"text","text":"turn %d answer"}]}}`+"\n", i+1, i+1)
		if err := os.WriteFile(logPath, []byte(line), 0o600); err != nil {
			t.Fatalf("WriteFile(%q): %v", logPath, err)
		}
		if i == 0 {
			if err := os.WriteFile(filepath.Join(dir, "1.prompt"), []byte("opening prompt"), 0o600); err != nil {
				t.Fatalf("WriteFile(1.prompt): %v", err)
			}
		}
		if err := st.RecordSpawn(ctx, storepkg.Spawn{
			RunID: plan.RunID(runID), Pgid: 99, StartedAt: testNow, LogPath: logPath,
		}); err != nil {
			t.Fatalf("RecordSpawn(%d): %v", runID, err)
		}
		exit := 0
		if err := st.RecordDisposition(ctx, storepkg.Disposition{
			RunID: plan.RunID(runID), Outcome: plan.OutcomePush, ExitCode: option.Some(exit),
			Cost: option.None[float64](), EndedAt: testNow,
		}); err != nil {
			t.Fatalf("RecordDisposition(%d): %v", runID, err)
		}
	}
	obs := plan.Observation{ObservedAt: testNow, Runs: map[plan.TicketURL]plan.RunObservation{sessionTicket: {}}}
	if err := st.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return st
}

func transcriptLines(t *testing.T, st *storepkg.Store) (prompt string, lines []string, you int) {
	t.Helper()
	page := htmltest.Parse(t, renderPath(t, newServer(st, testNow), "/s/acme/web/1"))
	for _, line := range page.Find(".line") {
		lines = append(lines, line.Text())
	}
	return page.One(t, "pre.prompt-text").Text(), lines, len(page.Find(".line-you"))
}

func testSessionTranscript(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		messages  []string
		wantLines []string
		wantYou   int
	}{
		{
			name: "reads as one conversation across runs", messages: []string{"", "also add <b>tests</b>", "ship it"},
			wantLines: []string{"turn 1 answer", "you also add <b>tests</b>", "turn 2 answer", "you ship it", "turn 3 answer"},
			wantYou:   2,
		},
		{name: "one run has no you line", messages: []string{""}, wantLines: []string{"turn 1 answer"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prompt, lines, you := transcriptLines(t, transcriptStore(t, tt.messages...))

			if prompt != "opening prompt" {
				t.Errorf("GET /s/acme/web/1 prompt = %q, want opening prompt", prompt)
			}
			if diff := cmp.Diff(tt.wantLines, lines); diff != "" {
				t.Errorf("GET /s/acme/web/1 lines mismatch (-want +got):\n%s", diff)
			}
			if you != tt.wantYou {
				t.Errorf("GET /s/acme/web/1 you lines = %d, want %d", you, tt.wantYou)
			}
		})
	}
}
