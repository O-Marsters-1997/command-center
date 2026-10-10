package web_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
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

func TestPageIsAWellFormedDocument(t *testing.T) {
	t.Parallel()

	now := testNow
	server := openServer(shellStore(t, &now, ""), fixedClock(now), "/data/fleet-hq")
	body := renderPath(t, server, "/tickets")

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

func TestShellIsLightOnlyAndMakesNoThirdPartyRequest(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(shellStore(t, &now, ""), now)
	body := renderPath(t, server, "/tickets")

	for _, gone := range []string{"data-theme", "toggleTheme", "localStorage", "prefers-color-scheme"} {
		if strings.Contains(body, gone) {
			t.Errorf("the page still carries %q:\n%s", gone, body)
		}
	}
	if strings.Contains(body, "https://") || strings.Contains(body, "//fonts.") {
		t.Errorf("the page requests a third-party origin:\n%s", body)
	}
}

func TestNavigationSwapsMainAndADirectLoadRendersTheFullLayout(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(shellStore(t, &now, ""), now)

	for _, path := range []string{"/tickets", "/insights", "/repos"} {
		body := renderPath(t, server, path)
		anchors := []string{`<aside class="sidebar"`, `<main id="main"`, `<ol class="crumbs">`, `class="topbar-action"`}
		for _, want := range anchors {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s is missing %q:\n%s", path, want, body)
			}
		}
	}

	body := renderPath(t, server, "/insights")
	for _, want := range []string{`hx-get="/insights"`, `hx-select="#main"`, `hx-target="#main"`, `hx-push-url="true"`} {
		if !strings.Contains(body, want) {
			t.Errorf("sidebar link is missing %q:\n%s", want, body)
		}
	}
}

func TestBareRootIsTheHomeScreenOnAPhone(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(shellStore(t, &now, ""), now)

	if body := renderPath(t, server, "/"); !strings.Contains(body, `<main id="main" data-home>`) {
		t.Errorf("the bare root is not marked as home:\n%s", body)
	}
	if body := renderPath(t, server, "/tickets"); strings.Contains(body, "data-home") {
		t.Errorf("a destination is marked as home:\n%s", body)
	}
	if body := renderPath(t, server, "/insights"); !strings.Contains(body, `class="back"`) {
		t.Errorf("an opened page has no back link:\n%s", body)
	}
}

func TestObserveChipReadsStalenessAtTwentySeconds(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	tests := []struct {
		name     string
		age      time.Duration
		never    bool
		wantChip string
	}{
		{name: "fresh", age: 2 * time.Second, wantChip: "observed 2s ago"},
		{name: "under twenty", age: 19 * time.Second, wantChip: "observed 19s ago"},
		{name: "at twenty", age: 20 * time.Second, wantChip: "last good observe: 20s ago"},
		{name: "well past", age: 5 * time.Minute, wantChip: "last good observe: 5m0s ago"},
		{name: "no observation", never: true, wantChip: "last good observe: never"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			at := &observedAt
			if tt.never {
				at = nil
			}
			now := observedAt.Add(tt.age)
			server := newServer(shellStore(t, at, ""), now)
			body := flattenTimes(renderPath(t, server, "/rail"))

			if !strings.Contains(body, tt.wantChip) {
				t.Errorf("chip does not read %q:\n%s", tt.wantChip, body)
			}
		})
	}
}

func TestRailBannerOnlyOpensOnAFailedTick(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	now := observedAt.Add(45 * time.Second)

	quiet := newServer(shellStore(t, &observedAt, ""), now)
	if body := renderPath(t, quiet, "/rail"); strings.Contains(body, "banner") {
		t.Errorf("a banner opened with no failed tick:\n%s", body)
	}

	failed := newServer(shellStore(t, &observedAt, "gh is unavailable"), now)
	body := flattenTimes(renderPath(t, failed, "/rail"))
	for _, want := range []string{"tick failed 44s ago", "gh is unavailable"} {
		if !strings.Contains(body, want) {
			t.Errorf("banner is missing %q:\n%s", want, body)
		}
	}
}

func TestRailBannerClosesOnceATickSucceeds(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	now := observedAt.Add(2 * time.Second)
	store := shellStore(t, &observedAt, "")
	tickErr := storepkg.TickError{At: observedAt.Add(-30 * time.Second), Message: "gh is unavailable"}
	if err := store.RecordTickError(t.Context(), tickErr); err != nil {
		t.Fatal(err)
	}

	body := flattenTimes(renderPath(t, newServer(store, now), "/rail"))
	if strings.Contains(body, "banner") {
		t.Errorf("the banner is still open after a tick recovered:\n%s", body)
	}
	if !strings.Contains(body, "observed 2s ago") {
		t.Errorf("the chip does not read quietly after a tick recovered:\n%s", body)
	}
}

func TestRailCountsLiveAgents(t *testing.T) {
	t.Parallel()

	now := testNow
	live := newServer(detailStore(t, writeLog(t, 1), now, now), now)
	if body := renderPath(t, live, "/rail"); !strings.Contains(body, "1 live") {
		t.Errorf("rail does not count the one live agent:\n%s", body)
	}

	idle := newServer(shellStore(t, &now, ""), now)
	body := renderPath(t, idle, "/rail")
	if !strings.Contains(body, "0 live") {
		t.Errorf("rail does not count zero live agents:\n%s", body)
	}
}

func TestRailCountsALiveRunWhoseRowReadsBaseGone(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	now := testNow
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

	body := renderPath(t, newServer(store, now), "/rail")
	if !strings.Contains(body, "1 live") {
		t.Errorf("rail does not count the live agent behind a base_gone row:\n%s", body)
	}
}

var timeTag = regexp.MustCompile(`<time datetime="[^"]*">([^<]*)</time>`)

func flattenTimes(s string) string { return timeTag.ReplaceAllString(s, "$1") }
