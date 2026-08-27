package cc_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// TestBoardSendsOnlyDestructiveVerbsToConfirm covers issue #75's AC1: needs_you offers re-run,
// kill and close-pr, and pr_merged offers remove-worktree, so the two states together show both
// halves of the split.
func TestBoardSendsOnlyDestructiveVerbsToConfirm(t *testing.T) {
	t.Parallel()

	body, err := cc.RenderStatesBoard([]plan.State{plan.NeedsYou, plan.PRMerged})
	if err != nil {
		t.Fatal(err)
	}

	wants := []string{
		`<form method="get" action="/confirm">`,
		`<button type="submit" name="verb" value="kill">kill</button>`,
		`<button type="submit" name="verb" value="remove-worktree">remove-worktree</button>`,
		`<form method="post" action="/verb">`,
		`<button type="submit" name="verb" value="re-run">re-run</button>`,
		`<button type="submit" name="verb" value="close-pr">close-pr</button>`,
	}
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("board does not contain %q:\n%s", want, body)
		}
	}
	if got := strings.Count(body, `action="/confirm"`); got != 2 {
		t.Errorf("confirm forms = %d, want 2: only kill and remove-worktree are destructive:\n%s", got, body)
	}
}

func TestConfirmNamesTheTaskTheVerbAndTheThingAtRisk(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Second)
	running := cc.Task{TicketURL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}

	tests := []struct {
		name  string
		verb  string
		store func(*testing.T) *cc.Store
		want  string
	}{
		{name: "kill names the pgid", verb: "kill", want: "4242",
			store: func(t *testing.T) *cc.Store { return runningRowStore(t, running, startedAt, now) }},
		{name: "remove-worktree names the path", verb: "remove-worktree", want: "/repos/cc-sandbox-cc-1-first",
			store: func(t *testing.T) *cc.Store { return seededStore(t, now) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := cc.NewServer(tt.store(t), fixedClock(now), nil)
			rec := httptest.NewRecorder()
			target := "/confirm?verb=" + tt.verb + "&task=sandbox://CC-1"
			server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
			}

			body := rec.Body.String()
			wants := []string{"sandbox://CC-1", tt.verb, tt.want,
				`<form method="post" action="/verb">`,
				`<input type="hidden" name="task" value="sandbox://CC-1">`,
				`<input type="hidden" name="verb" value="` + tt.verb + `">`,
				`<a href="/">`,
			}
			for _, want := range wants {
				if !strings.Contains(body, want) {
					t.Errorf("confirm page does not contain %q:\n%s", want, body)
				}
			}
		})
	}
}

// TestConfirmQueuesNothing covers issue #75's AC3: the page is a question, so rendering it is not
// half of a verb — only the [ confirm ] POST is.
func TestConfirmQueuesNothing(t *testing.T) {
	t.Parallel()

	store := seededStore(t, time.Now())
	server := cc.NewServer(store, time.Now, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/confirm?verb=remove-worktree&task=sandbox://CC-1", nil)
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	pending, err := store.PendingVerbIntents(t.Context(), "remove-worktree")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending remove-worktree intents = %+v, want none", pending)
	}
}

func TestConfirmRejectsNonDestructiveVerbsAndUnknownTasks(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil))
	t.Cleanup(srv.Close)

	tests := []struct{ name, query string }{
		{name: "a non-destructive verb needs no confirmation", query: "?verb=re-run&task=sandbox://CC-1"},
		{name: "an unsupported verb", query: "?verb=nope&task=sandbox://CC-1"},
		{name: "an unknown task", query: "?verb=kill&task=sandbox://NOPE"},
		{name: "a missing verb", query: "?task=sandbox://CC-1"},
		{name: "a missing task", query: "?verb=kill"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp, err := srv.Client().Get(srv.URL + "/confirm" + tt.query)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusBadRequest {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("status = %d, want 400: %s", resp.StatusCode, body)
			}
		})
	}
}
