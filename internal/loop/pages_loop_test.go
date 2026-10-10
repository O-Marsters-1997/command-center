package loop_test

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/loop"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func openServer(st *storepkg.Store, clock loop.Clock, dataDir string) *web.Server {
	server := web.NewServer(st, clock, dataDir)
	server.AllowAnonymous()
	return server
}

func renderPage(t *testing.T, server *web.Server) string {
	t.Helper()
	return renderPath(t, server, "/tickets")
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
	glyphTextRE  = regexp.MustCompile(`<span class="glyph[^"]*">([^<]*)</span>`)
	queuedVerbRE = regexp.MustCompile(`·\s*([\w-]+)\s*queued`)
)

func rowState(t *testing.T, page, ticketURL string) string {
	t.Helper()
	cell := rowCellAt(t, page, ticketURL, 1)
	glyph := glyphTextRE.FindStringSubmatch(cell)
	if glyph == nil {
		t.Fatalf("no state glyph found for %s in cell:\n%s", ticketURL, cell)
	}
	state := glyph[1]
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
	f := newLoopFixture(t,
		withTickets(sandboxTicket("1"), sandboxTicket("2"), sandboxTicket("3"), sandboxTicket("4")))
	for _, ticket := range f.Tickets {
		hash := plan.Hash(plan.Compose(plan.Ticket{URL: ticket.URL}))
		if err := f.Store.QueueLaunchIntent(t.Context(), ticket.URL, hash, "group-a", testAt); err != nil {
			t.Fatal(err)
		}
	}

	f.TickPastExplore(t)
	if len(f.ImplementSpawns()) != 1 {
		t.Fatalf("implement spawns after launch = %d, want 1: max_agents caps the rest as queued",
			len(f.ImplementSpawns()))
	}

	var runningTicket string
	for ticketURL, summary := range f.Latest(t) {
		if summary.Pgid != nil {
			runningTicket = ticketURL
		}
	}
	if runningTicket == "" {
		t.Fatal("no ticket recorded a run after tick 1")
	}
	runningPgid := *f.Latest(t)[runningTicket].Pgid

	var queuedSibling string
	for _, ticket := range f.Tickets {
		if ticket.URL != runningTicket {
			queuedSibling = ticket.URL
			break
		}
	}
	if err := f.Store.QueueVerbIntent(t.Context(), queuedSibling, "cancel", testAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	f.Tick(t)

	if len(f.ImplementSpawns()) != 1 {
		t.Errorf("implement spawns after tick 2 = %d, want still 1: a cancelled launch starts nothing",
			len(f.ImplementSpawns()))
	}
	if len(f.Fake.Canceled) != 0 {
		t.Errorf("canceled pgids = %v, want none: cancel never kills a live run", f.Fake.Canceled)
	}
	if !f.Fake.Alive[runningPgid] {
		t.Error("the running member's process was stopped; cancel must leave it running")
	}

	memberships, err := f.Store.LaunchMemberships(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, ticket := range f.Tickets {
		if !memberships[ticket.URL].Cancelled {
			t.Errorf("memberships = %+v, want %s cancelled: the whole launch was cancelled", memberships, ticket.URL)
		}
	}
	if f.Latest(t)[runningTicket].HasOutcome {
		t.Error("the running member was disposed; cancel must not touch a live run")
	}

	events, err := f.Store.Events(t.Context())
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

func TestLoopReconcilesATicketTheRepoScopeHides(t *testing.T) {
	hidden := storepkg.Ticket{URL: "sandbox://HIDDEN", Repo: "other", Branch: "hidden"}
	f := newLoopFixture(t, withMaxAgents(2), withExtraRepo("other"), withTickets(hidden))
	authoriseTicket(t, f.Store, hidden.URL, plan.Hash(plan.Compose(plan.Ticket{URL: hidden.URL})), testAt)
	f.TickPastExplore(t)

	if len(f.ImplementSpawns()) != 1 {
		t.Fatalf("implement spawns = %d, want 1: the loop must act on HIDDEN whether or not any view ever scopes it out",
			len(f.ImplementSpawns()))
	}
	if _, ok := f.Latest(t)[hidden.URL]; !ok {
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
