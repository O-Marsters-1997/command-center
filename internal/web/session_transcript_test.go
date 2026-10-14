package web_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func transcriptStore(t *testing.T, messages ...string) *storepkg.Store {
	t.Helper()
	ctx := t.Context()
	st := openStore(t)
	ticket := storepkg.Ticket{URL: sessionTicket, Repo: "acme/web", Branch: "cc-1", Feature: "checkout"}
	if err := st.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	session := storepkg.RunSession{StartNew: true}
	for i, message := range messages {
		kind := plan.RunKindFollowUp
		if i == 0 {
			kind = plan.RunKindAgent
		}
		session.Message = message
		runID, sessionID, err := st.InsertSessionRunSkeleton(ctx, sessionTicket, kind, "base", "hash", session)
		if err != nil {
			t.Fatal(err)
		}
		session = storepkg.RunSession{JoinID: sessionID}

		logPath := filepath.Join(dir, fmt.Sprintf("%d.jsonl", i+1))
		line := fmt.Sprintf(`{"type":"assistant","timestamp":"2026-08-20T12:00:0%dZ",`+
			`"message":{"content":[{"type":"text","text":"turn %d answer"}]}}`+"\n", i+1, i+1)
		if err := os.WriteFile(logPath, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if err := os.WriteFile(filepath.Join(dir, "1.prompt"), []byte("opening prompt"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.RecordSpawn(ctx, runID, 99, testNow, logPath); err != nil {
			t.Fatal(err)
		}
		exit := 0
		if err := st.RecordDisposition(
			ctx, runID, plan.OutcomePush, option.Some(exit), testNow, option.None[float64](),
		); err != nil {
			t.Fatal(err)
		}
	}
	obs := plan.Observation{ObservedAt: testNow, Runs: map[plan.TicketURL]plan.RunObservation{sessionTicket: {}}}
	if err := st.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSessionPageReadsAsOneConversationAcrossRuns(t *testing.T) {
	t.Parallel()
	st := transcriptStore(t, "", "also add <b>tests</b>", "ship it")
	body := renderPath(t, newServer(st, testNow), "/s/acme/web/1")

	wants := []string{
		"opening prompt",
		"turn 1 answer",
		`<span class="line-label">you</span> also add &lt;b&gt;tests&lt;/b&gt;`,
		"turn 2 answer",
		`<span class="line-label">you</span> ship it`,
		"turn 3 answer",
	}
	last := -1
	for _, want := range wants {
		i := strings.Index(body, want)
		if i < 0 {
			t.Fatalf("page lacks %q:\n%s", want, body)
		}
		if i < last {
			t.Errorf("%q renders before the line preceding it in %v", want, wants)
		}
		last = i
	}
	if n := strings.Count(body, `class="line line-you"`); n != 2 {
		t.Errorf("page has %d you lines, want 2", n)
	}
}

func TestSessionPageWithOneRunHasNoYouLine(t *testing.T) {
	t.Parallel()
	body := renderPath(t, newServer(transcriptStore(t, ""), testNow), "/s/acme/web/1")
	if strings.Contains(body, "line-you") || !strings.Contains(body, "turn 1 answer") {
		t.Errorf("single-run session page wrong:\n%s", body)
	}
}
