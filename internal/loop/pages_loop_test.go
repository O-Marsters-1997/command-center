package loop_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/loop"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func openServer(st *storepkg.Store, clock loop.Clock, dataDir string, opts ...web.Option) *web.Server {
	deps := web.Deps{Logger: slog.New(slog.DiscardHandler), Store: st, Clock: clock, DataDir: dataDir}
	return web.NewServer(deps, opts...)
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

func TestCancelLeavesARunningMemberUntouchedAndBlocksTheRest(t *testing.T) {
	t.Parallel()
	f := newLoopFixture(t, fixture{Tickets: []storepkg.Ticket{
		sandboxTicket("1"), sandboxTicket("2"), sandboxTicket("3"), sandboxTicket("4"),
	}})
	for _, ticket := range f.Tickets {
		hash := composeHash(ticket.URL)
		launch := storepkg.LaunchRequest{Ticket: ticket.URL, PromptHash: hash, Group: "group-a", At: testAt}
		if err := f.Store.QueueLaunchIntent(t.Context(), launch); err != nil {
			t.Fatalf("QueueLaunchIntent(%+v): %v", launch, err)
		}
	}

	f.Tick(t)
	if len(f.Fake.Spawns) != 1 {
		t.Fatalf("implement spawns after launch = %d, want 1: max_agents caps the rest as queued",
			len(f.Fake.Spawns))
	}

	var runningTicket string
	for ticketURL, summary := range f.Latest(t) {
		if summary.Pgid.IsSome() {
			runningTicket = ticketURL
		}
	}
	if runningTicket == "" {
		t.Fatal("no ticket recorded a run after tick 1")
	}
	runningPgid := f.Latest(t)[runningTicket].Pgid.Unwrap()

	var queuedSibling string
	for _, ticket := range f.Tickets {
		if ticket.URL != runningTicket {
			queuedSibling = ticket.URL
			break
		}
	}
	if err := f.Store.QueueVerbIntent(t.Context(), queuedSibling, "cancel", testAt.Add(time.Second)); err != nil {
		t.Fatalf("QueueVerbIntent(%q, cancel): %v", queuedSibling, err)
	}
	f.Tick(t)

	if len(f.Fake.Spawns) != 1 {
		t.Errorf("implement spawns after tick 2 = %d, want still 1: a cancelled launch starts nothing",
			len(f.Fake.Spawns))
	}
	if len(f.Fake.Canceled) != 0 {
		t.Errorf("canceled pgids = %v, want none: cancel never kills a live run", f.Fake.Canceled)
	}
	if !f.Fake.Alive[runningPgid] {
		t.Error("the running member's process was stopped; cancel must leave it running")
	}

	memberships, err := f.Store.LaunchMemberships(t.Context())
	if err != nil {
		t.Fatalf("LaunchMemberships(): %v", err)
	}
	for _, ticket := range f.Tickets {
		if !memberships[plan.TicketURL(ticket.URL)].Cancelled {
			t.Errorf("memberships = %+v, want %s cancelled: the whole launch was cancelled", memberships, ticket.URL)
		}
	}
	if f.Latest(t)[runningTicket].Outcome.IsSome() {
		t.Error("the running member was disposed; cancel must not touch a live run")
	}

	events, err := f.Store.Events(t.Context())
	if err != nil {
		t.Fatalf("Events(): %v", err)
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
	t.Parallel()
	hidden := storepkg.Ticket{URL: "sandbox://HIDDEN", Repo: "other", Branch: "hidden"}
	f := newLoopFixture(t, fixture{MaxAgents: 2, ExtraRepo: "other", Tickets: []storepkg.Ticket{hidden}})
	authoriseTicket(t, f.Store, hidden.URL, composeHash(hidden.URL), testAt)
	f.Tick(t)

	if len(f.Fake.Spawns) != 1 {
		t.Fatalf("implement spawns = %d, want 1: the loop must act on HIDDEN whether or not any view ever scopes it out",
			len(f.Fake.Spawns))
	}
	if _, ok := f.Latest(t)[hidden.URL]; !ok {
		t.Fatal("no run recorded for HIDDEN: the loop should have acted on it regardless of scope")
	}
}
