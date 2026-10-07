package web_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func shellStore(t *testing.T, observedAt *time.Time, tickErr string) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	if observedAt != nil {
		obs := plan.Observation{ObservedAt: *observedAt, PRs: map[string]plan.PR{}}
		if err := store.SaveObservation(ctx, obs); err != nil {
			t.Fatal(err)
		}
	}
	if tickErr != "" {
		if observedAt == nil {
			t.Fatal("a tick error is recorded against the observation's own instant, so it needs one")
			return nil
		}
		failedAt := observedAt.Add(time.Second)
		if err := store.RecordTickError(ctx, storepkg.TickError{At: failedAt, Message: tickErr}); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func boardFragment(t *testing.T, page string) string {
	t.Helper()

	start := strings.Index(page, `<table id="board"`)
	end := strings.Index(page, "</table>")
	if start < 0 || end < start {
		t.Fatalf("no board in page:\n%s", page)
	}
	return page[start:end]
}

func TestPageIsAWellFormedDocument(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := web.NewServer(shellStore(t, &now, ""), fixedClock(now), nil, "/data/fleet-hq")
	body := renderPage(t, server)

	if !strings.HasPrefix(body, "<!doctype html>\n<html lang=\"en\">\n<head>") {
		t.Errorf("page does not open a document:\n%s", body[:min(len(body), 200)])
	}
	for _, want := range []string{
		"<title>Command Centre</title>",
		"</head>",
		"<body>",
		"</body>",
		"</html>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, ">fleet-hq<") {
		t.Errorf("header does not name the workspace:\n%s", body)
	}
}

func TestThemeSitsOnTheRootElement(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := web.NewServer(shellStore(t, &now, ""), fixedClock(now), nil, "")
	body := renderPage(t, server)

	if strings.Contains(boardFragment(t, body), "data-theme") {
		t.Error("data-theme is inside the board, which htmx swaps")
	}
	headEnd := strings.Index(body, "</head>")
	if headEnd < 0 {
		t.Fatalf("page has no head:\n%s", body)
	}
	head := body[:headEnd]
	for _, want := range []string{
		"document.documentElement.dataset.theme",
		`localStorage.getItem("theme")`,
		`localStorage.setItem("theme"`,
		"prefers-color-scheme: dark",
	} {
		if !strings.Contains(head, want) {
			t.Errorf("the head script is missing %q:\n%s", want, head)
		}
	}
	if !strings.Contains(body, `onclick="toggleTheme()"`) {
		t.Errorf("the header has no theme toggle:\n%s", body)
	}
}

func TestObserveChipReadsStalenessAtTwentySeconds(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		age      time.Duration
		never    bool
		wantChip string
		stale    bool
	}{
		{name: "fresh", age: 2 * time.Second, wantChip: "observed 2s ago"},
		{name: "under twenty", age: 19 * time.Second, wantChip: "observed 19s ago"},
		{name: "at twenty", age: 20 * time.Second, wantChip: "last good observe: 20s ago", stale: true},
		{name: "well past", age: 5 * time.Minute, wantChip: "last good observe: 5m0s ago", stale: true},
		{name: "no observation", never: true, wantChip: "last good observe: never", stale: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			at := &observedAt
			if tt.never {
				at = nil
			}
			now := observedAt.Add(tt.age)
			server := web.NewServer(shellStore(t, at, ""), fixedClock(now), nil, "")
			body := flattenTimes(renderPage(t, server))

			if !strings.Contains(body, tt.wantChip) {
				t.Errorf("chip does not read %q:\n%s", tt.wantChip, body)
			}
			if got := strings.Contains(body, `data-tone="wait"`); got != tt.stale {
				t.Errorf("chip amber = %v, want %v:\n%s", got, tt.stale, body)
			}
		})
	}
}

func TestStaleBannerOnlyOpensOnAFailedTick(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := observedAt.Add(45 * time.Second)

	quiet := web.NewServer(shellStore(t, &observedAt, ""), fixedClock(now), nil, "")
	if body := renderPage(t, quiet); strings.Contains(body, "banner") {
		t.Errorf("a banner opened with no failed tick:\n%s", body)
	}

	failed := web.NewServer(shellStore(t, &observedAt, "gh is unavailable"), fixedClock(now), nil, "")
	body := flattenTimes(renderPage(t, failed))
	for _, want := range []string{
		"the last tick failed 44s ago",
		"nothing below has been re-derived since",
		"gh is unavailable",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("banner is missing %q:\n%s", want, body)
		}
	}
}

func TestStaleBannerClosesOnceATickSucceeds(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := observedAt.Add(2 * time.Second)
	store := shellStore(t, &observedAt, "")
	tickErr := storepkg.TickError{At: observedAt.Add(-30 * time.Second), Message: "gh is unavailable"}
	if err := store.RecordTickError(t.Context(), tickErr); err != nil {
		t.Fatal(err)
	}

	body := flattenTimes(renderPage(t, web.NewServer(store, fixedClock(now), nil, "")))
	if strings.Contains(body, "banner") {
		t.Errorf("the banner is still open after a tick recovered:\n%s", body)
	}
	if !strings.Contains(body, "observed 2s ago") {
		t.Errorf("the chip does not read quietly after a tick recovered:\n%s", body)
	}
}

func TestHeaderCountsLiveAgents(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	live := web.NewServer(detailStore(t, writeLog(t, 1), now, now), fixedClock(now), nil, "")
	if body := renderPage(t, live); !strings.Contains(body, "1 live") {
		t.Errorf("header does not count the one live agent:\n%s", body)
	}

	idle := web.NewServer(shellStore(t, &now, ""), fixedClock(now), nil, "")
	body := renderPage(t, idle)
	if !strings.Contains(body, "0 live") {
		t.Errorf("header does not count zero live agents:\n%s", body)
	}
	if strings.Contains(body, `id="liveness"`) {
		t.Errorf("the liveness div is still on the page:\n%s", body)
	}
}

func TestHeaderCountsALiveRunWhoseRowReadsBaseGone(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := openStore(t)
	blocker := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "repo", Branch: "cc-1"}
	dependent := storepkg.Ticket{
		URL: "sandbox://CC-2", Repo: "repo", Branch: "cc-2", BlockedBy: []string{blocker.URL},
	}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{blocker, dependent}); err != nil {
		t.Fatal(err)
	}
	runID, err := store.InsertRunSkeleton(ctx, dependent.URL, "agent", "basesha1234", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, runID, 4242, now, "/logs/run.jsonl"); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observation{
		ObservedAt: now,
		Runs:       map[string]plan.RunObservation{dependent.URL: {Alive: true}},
		PRs:        map[string]plan.PR{"cc-1": {State: plan.Closed}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	body := renderPage(t, web.NewServer(store, fixedClock(now), nil, ""))
	if !strings.Contains(body, "1 live") {
		t.Errorf("header does not count the live agent behind a base_gone row:\n%s", body)
	}
}

func TestHeaderRefreshesWithTheBoard(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := web.NewServer(shellStore(t, &now, ""), fixedClock(now), nil, "")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Body.String(); !strings.Contains(got, `id="masthead" hx-swap-oob="true"`) {
		t.Errorf("the masthead is not an out-of-band swap target:\n%s", got)
	}
}

var timeTag = regexp.MustCompile(`<time datetime="[^"]*">([^<]*)</time>`)

func flattenTimes(s string) string { return timeTag.ReplaceAllString(s, "$1") }
