package web_test

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func seedRun(t *testing.T, store *storepkg.Store, ticketURL string) int64 {
	t.Helper()
	runID, err := store.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{
		Ticket: ticketURL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%q): %v", ticketURL, err)
	}
	if err := store.RecordSpawn(t.Context(), storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: testNow, LogPath: "/state/runs/1.jsonl",
	}); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	return runID
}

func seededRunning(t *testing.T) *storepkg.Store {
	t.Helper()

	store := seededStore(t, testNow)
	seedRun(t, store, "sandbox://CC-1")
	obs, _, err := store.LastObservation(t.Context())
	if err != nil {
		t.Fatalf("LastObservation: %v", err)
	}
	obs.Runs = map[plan.TicketURL]plan.RunObservation{"sandbox://CC-1": {Alive: true}}
	if err := store.SaveObservation(t.Context(), obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func seededFailed(t *testing.T) *storepkg.Store {
	t.Helper()

	store := seededStore(t, testNow)
	runID := seedRun(t, store, "sandbox://CC-1")
	if err := store.RecordDisposition(t.Context(), storepkg.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.OutcomeFailed, ExitCode: option.Some(1),
		Cost: option.None[float64](), EndedAt: testNow,
	}); err != nil {
		t.Fatalf("RecordDisposition(%d): %v", runID, err)
	}
	return store
}

func seededQueued(t *testing.T) *storepkg.Store {
	t.Helper()

	store := seededStore(t, testNow)
	if err := store.QueueLaunchIntent(t.Context(), storepkg.LaunchRequest{
		Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-1", At: testNow,
	}); err != nil {
		t.Fatalf("QueueLaunchIntent: %v", err)
	}
	if err := store.ApplyLaunchIntents(t.Context(), testNow); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}
	return store
}

func seededStore(t *testing.T, observedAt time.Time) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
	}
	storetest.Tickets(t, store, tickets...)

	obs := plan.Observation{
		ObservedAt: observedAt,
		PRs:        map[plan.BranchRef]plan.PR{},
		Worktrees:  map[plan.BranchRef]string{branchRef("cc-sandbox", "cc-1-first"): "/repos/cc-sandbox-cc-1-first"},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	tickErr := storepkg.TickError{At: observedAt.Add(15 * time.Second), Message: "observe: gh pr list: exit status 1"}
	if err := store.RecordTickError(ctx, tickErr); err != nil {
		t.Fatalf("RecordTickError: %v", err)
	}
	return store
}

func seededServer(t *testing.T) *web.Server {
	t.Helper()

	store := seededStore(t, testNow)
	if err := store.QueueLaunchIntent(t.Context(), storepkg.LaunchRequest{
		Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: testNow,
	}); err != nil {
		t.Fatalf("QueueLaunchIntent: %v", err)
	}
	return newServer(store, testNow.Add(45*time.Second))
}

func seededStoreAtTestNow(t *testing.T) *storepkg.Store {
	t.Helper()
	return seededStore(t, testNow)
}

func pendingMutationIntentCount(t *testing.T, store *storepkg.Store) int {
	t.Helper()
	byTicket, err := store.PendingIntents(t.Context())
	if err != nil {
		t.Fatalf("PendingIntents: %v", err)
	}
	count := 0
	for _, intents := range byTicket {
		count += len(intents)
	}
	edits, err := store.PendingEditTicketIntents(t.Context())
	if err != nil {
		t.Fatalf("PendingEditTicketIntents: %v", err)
	}
	return count + len(edits)
}

func runningRowStore(t *testing.T, ticket storepkg.Ticket, startedAt, now time.Time) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	storetest.Tickets(t, store, ticket)
	runID, err := store.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: ticket.URL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatalf("InsertRunSkeleton(%q): %v", ticket.URL, err)
	}
	if err := store.RecordSpawn(ctx, storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: startedAt, LogPath: "/state/runs/1.jsonl",
	}); err != nil {
		t.Fatalf("RecordSpawn(%d): %v", runID, err)
	}
	obs := plan.Observation{
		ObservedAt: now,
		Runs:       map[plan.TicketURL]plan.RunObservation{plan.TicketURL(ticket.URL): {Alive: true}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func requireBuiltStylesheet(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("assets/dist/app.css"); errors.Is(err, fs.ErrNotExist) {
		t.Skip("assets/dist/app.css is not built; run `just assets`")
	}
}

var mutationRoutes = []struct {
	name   string
	target string
	form   url.Values
	seed   func(*testing.T) *storepkg.Store
}{
	{name: "launch", target: "/launch?ticket=sandbox://CC-1", seed: seededStoreAtTestNow},
	{name: "verb", target: "/verb?verb=kill&ticket=sandbox://CC-1", seed: seededRunning},
	{name: "ticket", target: "/ticket", form: url.Values{
		"ticket": {"sandbox://CC-2"}, "branch": {"cc-2-second"},
	}, seed: seededStoreAtTestNow},
	{name: "launch/open", target: "/launch/open", form: url.Values{"feature": {"x"}}, seed: openStore},
	{name: "feature import", target: "/features/project:x/import", seed: openStore},
}

func TestServer(t *testing.T) {
	t.Parallel()

	t.Run("unknown and retired paths are 404", func(t *testing.T) {
		t.Parallel()
		server := newServer(seededStore(t, testNow), testNow)
		for _, tt := range []struct{ method, path string }{
			{http.MethodGet, "/nope"},
			{http.MethodGet, "/confirm?verb=kill&ticket=sandbox://CC-1"},
			{http.MethodGet, "/preview?ticket=sandbox://CC-1"},
			{http.MethodGet, "/ticket/" + url.PathEscape("sandbox://CC-1") + "/detail"},
			{http.MethodGet, "/task/" + url.PathEscape("sandbox://CC-1") + "/detail"},
			{http.MethodGet, "/page.css"},
			{http.MethodGet, "/import"},
			{http.MethodPost, "/import"},
			{http.MethodGet, "/login"},
			{http.MethodPost, "/login"},
			{http.MethodPost, "/logout"},
		} {
			t.Run(tt.method+" "+tt.path, func(t *testing.T) {
				t.Parallel()
				if got := do(t, server, tt.method, tt.path, req{}); got.Code != http.StatusNotFound {
					t.Errorf("%s %s status = %d, want 404", tt.method, tt.path, got.Code)
				}
			})
		}
	})

	t.Run("pages serve without a cookie", func(t *testing.T) {
		t.Parallel()
		server := newServer(seededStore(t, testNow), testNow)
		if got := do(t, server, http.MethodGet, "/tickets", req{}); got.Code != http.StatusOK {
			t.Errorf("GET /tickets without a cookie status = %d, want 200", got.Code)
		}
	})

	t.Run("a server error hides its cause and logs it", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		var logs bytes.Buffer
		server := web.NewServer(web.Deps{
			Logger: slog.New(slog.NewTextHandler(&logs, nil)), Store: st, Clock: fixedClock(testNow),
		})
		if err := st.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		rec := do(t, server, http.MethodGet, "/tickets", req{})
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("GET /tickets on a closed store status = %d, want 500", rec.Code)
		}
		if strings.Contains(rec.Body, "closed") {
			t.Errorf("GET /tickets 500 body = %q, want the cause hidden", rec.Body)
		}
		if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "closed") {
			t.Errorf("GET /tickets 500 log = %q, want an error naming the cause", logs.String())
		}
	})

	for _, route := range mutationRoutes {
		t.Run(route.name+" checks the origin", func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name, origin string
				wantStatus   int
				wantIntents  int
			}{
				{name: "foreign", origin: "http://evil.example", wantStatus: http.StatusForbidden},
				{name: "missing", origin: "-", wantStatus: http.StatusSeeOther, wantIntents: 1},
				{name: "same", wantStatus: http.StatusSeeOther, wantIntents: 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					store := route.seed(t)
					before := pendingMutationIntentCount(t, store)
					beforeTickets, err := store.Tickets(t.Context())
					if err != nil {
						t.Fatalf("Tickets(before POST %s): %v", route.target, err)
					}

					got := do(t, newServer(store, testNow), http.MethodPost, route.target, req{
						Form: route.form, Origin: tc.origin,
					})
					if got.Code != tc.wantStatus {
						t.Errorf("POST %s origin %s status = %d, want %d", route.target, tc.name, got.Code, tc.wantStatus)
					}
					if after := pendingMutationIntentCount(t, store); after != before+tc.wantIntents {
						t.Errorf("POST %s origin %s pending intents = %d, want %d", route.target, tc.name, after, before+tc.wantIntents)
					}
					afterTickets, err := store.Tickets(t.Context())
					if err != nil {
						t.Fatalf("Tickets(after POST %s): %v", route.target, err)
					}
					if diff := cmp.Diff(beforeTickets, afterTickets); diff != "" {
						t.Errorf("POST %s changed tickets before reconciliation (-before +after):\n%s", route.target, diff)
					}
				})
			}
		})

		t.Run(route.name+" refuses a wrong method", func(t *testing.T) {
			t.Parallel()
			server := newServer(openStore(t), testNow)
			if got := do(t, server, http.MethodPut, route.target, req{}); got.Code != http.StatusMethodNotAllowed {
				t.Errorf("PUT %s status = %d, want 405", route.target, got.Code)
			}
		})
	}

	t.Run("serves the built stylesheet", func(t *testing.T) {
		t.Parallel()
		requireBuiltStylesheet(t)

		got := do(t, newServer(seededStore(t, testNow), testNow), http.MethodGet, "/assets/app.css", req{})
		if got.Code != http.StatusOK {
			t.Fatalf("GET /assets/app.css status = %d, want 200", got.Code)
		}
		if contentType := got.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/css") {
			t.Errorf("GET /assets/app.css Content-Type = %q, want text/css", contentType)
		}
	})

	t.Run("serves htmx from the binary", func(t *testing.T) {
		t.Parallel()
		server := newServer(seededStore(t, testNow), testNow)
		for _, asset := range []string{"/assets/htmx.min.js", "/assets/sse.min.js"} {
			if rec := do(t, server, http.MethodGet, asset, req{}); rec.Code != http.StatusOK {
				t.Errorf("GET %s status = %d, want 200", asset, rec.Code)
			}
		}
	})
}
