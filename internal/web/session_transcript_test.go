package web_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
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
			Ticket: sessionTicket, Kind: kind, BaselineSHA: "base", PromptHash: "hash", Session: session,
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

func TestSessionPageReadsAsOneConversationAcrossRuns(t *testing.T) {
	t.Parallel()
	st := transcriptStore(t, "", "also add <b>tests</b>", "ship it")
	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1")

	page := htmltest.Parse(t, body)
	if got := page.One(t, "pre.prompt-text").Text(); got != "opening prompt" {
		t.Errorf("opening prompt = %q", got)
	}
	lines := page.Find(".line")
	var got []string
	for _, line := range lines {
		got = append(got, line.Text())
	}
	want := []string{"turn 1 answer", "you also add <b>tests</b>", "turn 2 answer", "you ship it", "turn 3 answer"}
	if !slices.Equal(got, want) {
		t.Errorf("session lines = %q, want %q", got, want)
	}
	if you := htmltest.Parse(t, body).Find(".line-you"); len(you) != 2 {
		t.Errorf("you lines = %d, want 2", len(you))
	}
}

func TestSessionPageWithOneRunHasNoYouLine(t *testing.T) {
	t.Parallel()
	body := renderPath(t, newServer(transcriptStore(t, ""), testNow), "/s/acme/web/1")
	page := htmltest.Parse(t, body)
	if lines := page.Find(".line"); len(lines) == 0 {
		t.Error("single-run session has no transcript lines")
	} else if got := lines[0].Text(); got != "turn 1 answer" {
		t.Errorf("single-run transcript = %q: %s", got, lines[0].HTML())
	}
	if you := page.Find(".line-you"); len(you) != 0 {
		t.Errorf("single-run session has a you line: %s", you[0].HTML())
	}
}
