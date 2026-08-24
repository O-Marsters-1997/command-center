package cc_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func renderPage(t *testing.T, server *cc.Server) string {
	t.Helper()
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("render page: status = %d: %s", rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func rowState(t *testing.T, page, ticketURL string) string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta("<td>"+ticketURL+"</td>") + `\s*<td>([^<]*)</td>`)
	m := re.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no row found for %s in page:\n%s", ticketURL, page)
	}
	return m[1]
}

// rowCellAt returns one row's cell content, counting the ticket cell itself as column 0 — the
// <tr> layout page.tmpl renders (docs/prds/prd-command-centre.md § The page).
func rowCellAt(t *testing.T, page, ticketURL string, column int) string {
	t.Helper()
	re := regexp.MustCompile(`(?s)` + regexp.QuoteMeta("<td>"+ticketURL+"</td>") +
		strings.Repeat(`\s*<td>.*?</td>`, column-1) + `\s*<td>([^<]*)</td>`)
	m := re.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no column %d found for %s in page:\n%s", column, ticketURL, page)
	}
	return m[1]
}

func TestCancelLeavesARunningMemberUntouchedAndBlocksTheRest(t *testing.T) {
	root, _ := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)

	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))

	tickets := []string{"sandbox://CC-1", "sandbox://CC-2", "sandbox://CC-3", "sandbox://CC-4"}
	tasks := make([]cc.Task, len(tickets))
	for i, ticketURL := range tickets {
		branch := strings.TrimPrefix(ticketURL, "sandbox://")
		tasks[i] = cc.Task{TicketURL: ticketURL, Repo: "repo", Branch: strings.ToLower(branch)}
	}
	if err := store.UpsertTasks(t.Context(), tasks); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for _, ticketURL := range tickets {
		hash := plan.Hash(plan.Compose(plan.Task{TicketURL: ticketURL}, nil))
		if err := store.QueueLaunchIntent(t.Context(), ticketURL, hash, "group-a", at); err != nil {
			t.Fatal(err)
		}
	}

	fake := newFakeRunner()
	loop := cc.NewLoop(store, noOpObserve, fixedClock(at), cfg, ws, fake)
	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if len(fake.spawns) != 1 {
		t.Fatalf("spawns after tick 1 = %d, want 1: max_agents caps the rest as queued", len(fake.spawns))
	}

	latest, err := store.LatestRunsByTask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var runningTicket string
	for ticketURL, summary := range latest {
		if summary.Pgid != nil {
			runningTicket = ticketURL
		}
	}
	if runningTicket == "" {
		t.Fatal("no task recorded a run after tick 1")
	}
	runningPgid := *latest[runningTicket].Pgid

	var queuedSibling string
	for _, ticketURL := range tickets {
		if ticketURL != runningTicket {
			queuedSibling = ticketURL
			break
		}
	}
	if err := store.QueueVerbIntent(t.Context(), queuedSibling, "cancel", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	if err := loop.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}

	if len(fake.spawns) != 1 {
		t.Errorf("spawns after tick 2 = %d, want still 1: launchEligible must start nothing from a cancelled launch",
			len(fake.spawns))
	}
	if len(fake.canceled) != 0 {
		t.Errorf("canceled pgids = %v, want none: cancel never kills a live run", fake.canceled)
	}
	if !fake.alive[runningPgid] {
		t.Error("the running member's process was stopped; cancel must leave it running")
	}

	memberships, err := store.LaunchMemberships(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, ticketURL := range tickets {
		if !memberships[ticketURL].Cancelled {
			t.Errorf("memberships = %+v, want %s cancelled: the whole launch was cancelled", memberships, ticketURL)
		}
	}

	server := cc.NewServer(store, fixedClock(at.Add(2*time.Second)), cfg.Repos, nil, "")
	page := renderPage(t, server)
	if state := rowState(t, page, runningTicket); state != "running" {
		t.Errorf("running member's rendered state = %q, want running: a row that ever ran is never cancelled", state)
	}
	if state := rowState(t, page, queuedSibling); state != "cancelled" {
		t.Errorf("queued sibling's rendered state = %q, want cancelled", state)
	}

	latestAfter, err := store.LatestRunsByTask(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if latestAfter[runningTicket].HasOutcome {
		t.Error("the running member was disposed; cancel must not touch a live run")
	}

	events, err := store.Events(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var sawCancelEvent bool
	for _, e := range events {
		if e.Kind == "launch_cancelled" {
			sawCancelEvent = true
			if !strings.Contains(e.Detail, "4") {
				t.Errorf("launch_cancelled detail = %q, want it to name 4 members", e.Detail)
			}
		}
	}
	if !sawCancelEvent {
		t.Errorf("events = %+v, want a launch_cancelled event", events)
	}
}
