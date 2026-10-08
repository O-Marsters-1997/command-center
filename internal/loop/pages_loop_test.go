package loop_test

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"

	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

func renderPage(t *testing.T, server *web.Server) string {
	t.Helper()
	return renderPath(t, server, "/")
}

func renderPath(t *testing.T, server *web.Server, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d: %s", path, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func ticketRef(ticketURL string) string { return "#" + path.Base(ticketURL) }

// rowHTML finds the whole <tr>...</tr> whose ticket-link button names ticketURL. Go's RE2 engine
// has no lookahead to keep a lazy ".*?" from crossing a row boundary, so this splits on literal
// "<tr" instead of matching in one regexp.
func rowHTML(t *testing.T, page, ticketURL string) string {
	t.Helper()
	ref := ticketRef(ticketURL)
	for _, block := range strings.Split(page, "<tr") {
		if !strings.Contains(block, ref) {
			continue
		}
		end := strings.Index(block, "</tr>")
		if end < 0 {
			continue
		}
		return "<tr" + block[:end+len("</tr>")]
	}
	t.Fatalf("no row found for %s in page:\n%s", ticketURL, page)
	return ""
}

var (
	pillTextRE   = regexp.MustCompile(`<span class="pill[^"]*">([^<]*)</span>`)
	queuedVerbRE = regexp.MustCompile(`·\s*([\w-]+)\s*queued`)
)

func rowState(t *testing.T, page, ticketURL string) string {
	t.Helper()
	cell := rowCellAt(t, page, ticketURL, 1)
	pill := pillTextRE.FindStringSubmatch(cell)
	if pill == nil {
		t.Fatalf("no state pill found for %s in cell:\n%s", ticketURL, cell)
	}
	state := pill[1]
	for _, m := range queuedVerbRE.FindAllStringSubmatch(cell, -1) {
		state += " · " + m[1] + " queued"
	}
	return state
}

func rowCellAt(t *testing.T, page, ticketURL string, column int) string {
	t.Helper()
	row := rowHTML(t, page, ticketURL)
	cells := regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`).FindAllStringSubmatch(row, -1)
	if column >= len(cells) {
		t.Fatalf("no column %d found for %s in page:\n%s", column, ticketURL, page)
	}
	return strings.TrimSpace(cells[column][1])
}

func TestCancelLeavesARunningMemberUntouchedAndBlocksTheRest(t *testing.T) {
	root, _ := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)

	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	store := openStore(t)

	ticketURLs := []string{"sandbox://CC-1", "sandbox://CC-2", "sandbox://CC-3", "sandbox://CC-4"}
	tickets := make([]storepkg.Ticket, len(ticketURLs))
	for i, ticketURL := range ticketURLs {
		branch := strings.TrimPrefix(ticketURL, "sandbox://")
		tickets[i] = storepkg.Ticket{URL: ticketURL, Repo: "repo", Branch: strings.ToLower(branch)}
	}
	if err := store.UpsertTickets(t.Context(), tickets); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for _, ticketURL := range ticketURLs {
		hash := plan.Hash(plan.Compose(plan.Ticket{URL: ticketURL}))
		if err := store.QueueLaunchIntent(t.Context(), ticketURL, hash, "group-a", at); err != nil {
			t.Fatal(err)
		}
	}

	fake := runner.NewFake()
	lp := loop.NewLoop(store, noOpObserve, fixedClock(at), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns after tick 1 = %d, want 1: max_agents caps the rest as queued", len(fake.Spawns))
	}

	latest, err := store.LatestRunsByTicket(t.Context())
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
		t.Fatal("no ticket recorded a run after tick 1")
	}
	runningPgid := *latest[runningTicket].Pgid

	var queuedSibling string
	for _, ticketURL := range ticketURLs {
		if ticketURL != runningTicket {
			queuedSibling = ticketURL
			break
		}
	}
	if err := store.QueueVerbIntent(t.Context(), queuedSibling, "cancel", at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}

	if len(fake.Spawns) != 1 {
		t.Errorf("spawns after tick 2 = %d, want still 1: launchEligible must start nothing from a cancelled launch",
			len(fake.Spawns))
	}
	if len(fake.Canceled) != 0 {
		t.Errorf("canceled pgids = %v, want none: cancel never kills a live run", fake.Canceled)
	}
	if !fake.Alive[runningPgid] {
		t.Error("the running member's process was stopped; cancel must leave it running")
	}

	memberships, err := store.LaunchMemberships(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, ticketURL := range ticketURLs {
		if !memberships[ticketURL].Cancelled {
			t.Errorf("memberships = %+v, want %s cancelled: the whole launch was cancelled", memberships, ticketURL)
		}
	}

	latestAfter, err := store.LatestRunsByTicket(t.Context())
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

// TestLoopReconcilesATicketTheRepoScopeHides covers issue #219 AC6: the loop never reads a view
// parameter, so a tick still authorises and spawns a ticket that a repo scope's own board would
// never show.
func TestLoopReconcilesATicketTheRepoScopeHides(t *testing.T) {
	root, _ := repoWithOrigin(t)
	installFakeTp(t, false)
	installFakeGh(t, false)

	cfg, ws := testConfigAndWorkspace(t, root, 2, []string{"true"})
	cfg.Repos = append(cfg.Repos, config.Repo{Name: "other", Checkout: filepath.Join(root, "repo")})

	store := openStore(t)
	shown := storepkg.Ticket{URL: "sandbox://SHOWN", Repo: "repo", Branch: "shown"}
	hidden := storepkg.Ticket{URL: "sandbox://HIDDEN", Repo: "other", Branch: "hidden"}
	if err := store.UpsertTickets(t.Context(), []storepkg.Ticket{shown, hidden}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	hash := plan.Hash(plan.Compose(plan.Ticket{URL: hidden.URL}))
	authoriseTicket(t, store, hidden.URL, hash, at)

	fake := runner.NewFake()
	lp := loop.NewLoop(store, noOpObserve, fixedClock(at), cfg, ws, fake)
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(fake.Spawns) != 1 {
		t.Fatalf("spawns = %d, want 1: the loop must act on HIDDEN whether or not any view ever scopes it out",
			len(fake.Spawns))
	}
	latest, err := store.LatestRunsByTicket(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := latest[hidden.URL]; !ok {
		t.Fatal("no run recorded for HIDDEN: the loop should have acted on it regardless of scope")
	}
}

var update = flag.Bool("update", false, "regenerate golden files")

const goldenPrompt = "testdata/prompt.golden.txt"

// assertGolden compares got against the golden file at path, rewriting it under -update.
func assertGolden(t *testing.T, path string, got []byte) {
	t.Helper()

	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (regenerate with -update): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("render differs from %s; rerun with -update to accept\n--- got ---\n%s", path, got)
	}
}

func selPagePath(ticketURL string) string {
	return "/?" + url.Values{"sel": {ticketURL}}.Encode()
}
