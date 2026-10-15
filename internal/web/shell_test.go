package web_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
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
		obs := plan.Observation{ObservedAt: *observedAt, PRs: map[plan.BranchRef]plan.PR{}}
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
	for _, closing := range []string{"</head>", "</body>", "</html>"} {
		if !strings.Contains(body, closing) {
			t.Errorf("document is missing closing tag %q", closing)
		}
	}
	root := htmltest.Parse(t, body)
	root.One(t, "head title")
	if got := root.One(t, "title").Text(); got != "Tickets · Command Centre" {
		t.Errorf("title = %q", got)
	}
	if got := root.One(t, "body").Text(); !strings.Contains(got, "fleet-hq") {
		t.Error("workspace name fleet-hq absent from body text")
	}
}

func TestShellIsLightOnlyAndMakesNoThirdPartyRequest(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(shellStore(t, &now, ""), now)
	body := renderPath(t, server, "/tickets")

	root := htmltest.Parse(t, body)
	if themed := root.Find("[data-theme]"); len(themed) != 0 {
		t.Errorf("light-only page carries a theme toggle: %s", themed[0].HTML())
	}
	for _, script := range root.Find("script") {
		for _, mechanism := range []string{"toggleTheme", "localStorage", "prefers-color-scheme"} {
			if strings.Contains(script.Text(), mechanism) {
				t.Errorf("inline theme mechanism %q in %s", mechanism, script.HTML())
			}
		}
	}
	for _, resource := range []struct{ selector, attr string }{
		{"script[src]", "src"}, {"link[href]", "href"}, {"base[href]", "href"},
		{"img[src]", "src"}, {"img[srcset]", "srcset"},
		{"source[src]", "src"}, {"source[srcset]", "srcset"},
		{"iframe[src]", "src"}, {"frame[src]", "src"},
		{"video[src]", "src"}, {"video[poster]", "poster"},
		{"audio[src]", "src"}, {"track[src]", "src"},
		{"embed[src]", "src"}, {"object[data]", "data"},
		{"input[src]", "src"}, {"image[href]", "href"}, {"use[href]", "href"},
	} {
		for _, node := range root.Find(resource.selector) {
			value, _ := node.Attr(resource.attr)
			urls := []string{value}
			if resource.attr == "srcset" {
				urls = nil
				for _, candidate := range strings.Split(value, ",") {
					if fields := strings.Fields(candidate); len(fields) > 0 {
						urls = append(urls, fields[0])
					}
				}
			}
			for _, url := range urls {
				if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "//") {
					t.Errorf("shell resource %s %s = %q requests a third-party origin", resource.selector, resource.attr, url)
				}
			}
		}
	}
}

func TestNavigationSwapsMainAndADirectLoadRendersTheFullLayout(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(shellStore(t, &now, ""), now)

	for _, tt := range []struct{ name, path string }{
		{name: "tickets", path: "/tickets"},
		{name: "insights", path: "/insights"},
		{name: "repos", path: "/repos"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := renderPath(t, server, tt.path)
			root := htmltest.Parse(t, body)
			for _, selector := range []string{"aside#sidebar", "main#main", "ol.crumbs", ".topbar-action"} {
				root.One(t, selector)
			}
		})
	}

	root := htmltest.Parse(t, renderPath(t, server, "/insights"))
	link := root.One(t, `nav#sidebar-nav a[href="/insights"]`)
	attrs := map[string]string{
		"hx-get": "/insights", "hx-select": "#main",
		"hx-target": "#main", "hx-push-url": "true",
	}
	for name, want := range attrs {
		if got, _ := link.Attr(name); got != want {
			t.Errorf("sidebar %s = %q, want %q: %s", name, got, want, link.HTML())
		}
	}
}

func TestBareRootIsTheHomeScreenOnAPhone(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(shellStore(t, &now, ""), now)

	root := htmltest.Parse(t, renderPath(t, server, "/")).One(t, "main#main")
	if _, ok := root.Attr("data-home"); !ok {
		t.Error("home main lacks data-home")
	}
	if _, ok := root.Attr("data-desktop-target"); !ok {
		t.Error("home main lacks desktop target")
	}
	main := htmltest.Parse(t, renderPath(t, server, "/tickets")).One(t, "main#main")
	if _, ok := main.Attr("data-home"); ok {
		t.Error("tickets main is marked home")
	}
	htmltest.Parse(t, renderPath(t, server, "/insights")).One(t, `main#main a[href="/"]`)
}

func TestHomePageCarriesTheGoToKeyAnchors(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(shellStore(t, &now, ""), now)
	body := renderPath(t, server, "/")

	root := htmltest.Parse(t, body)
	anchors := map[string]string{
		"g h": "/", "g t": "/tickets", "g i": "/insights",
		"g r": "/repos", "c": "/launch",
	}
	for key, href := range anchors {
		root.One(t, `a[data-key="`+key+`"][href="`+href+`"]`)
	}
	root.One(t, `input#rail-search[data-key="mod+k"]`)
	root.One(t, `script[type="module"][src="/assets/dist/keys.js"]`)
}

func TestEveryPageCarriesTheShortcutSheetOutsideMain(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(shellStore(t, &now, ""), now)

	for _, tt := range []struct{ name, path string }{
		{name: "home", path: "/"},
		{name: "tickets", path: "/tickets"},
		{name: "insights", path: "/insights"},
		{name: "repos", path: "/repos"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := htmltest.Parse(t, renderPath(t, server, tt.path))
			root.One(t, "main#main")
			sidebar := root.One(t, "aside#sidebar")
			sidebar.One(t, `button[data-key="?"]`)
			sheet := sidebar.One(t, "dialog#key-sheet")
			var got []string
			for _, row := range sheet.Find(".key-row") {
				got = append(got, row.One(t, "kbd").Text()+" "+row.One(t, "dd").Text())
			}
			var want []string
			for _, group := range view.Keymap {
				if !strings.Contains(sheet.Text(), group.Title) {
					t.Errorf("shortcut sheet lacks heading %q: %s", group.Title, sheet.HTML())
				}
				for _, row := range group.Rows {
					want = append(want, row.Keys+" "+row.Does)
				}
			}
			if !slices.Equal(got, want) {
				t.Errorf("shortcut rows = %q, want %q", got, want)
			}
		})
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
			chip := htmltest.Parse(t, renderPath(t, server, "/rail")).One(t, ".rail-health")
			if got := chip.Text(); !strings.Contains(got, tt.wantChip) {
				t.Errorf("chip = %q, want %q: %s", got, tt.wantChip, chip.HTML())
			}
		})
	}
}

func TestRailBannerOnlyOpensOnAFailedTick(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	now := observedAt.Add(45 * time.Second)

	quiet := newServer(shellStore(t, &observedAt, ""), now)
	quietRail := htmltest.Parse(t, renderPath(t, quiet, "/rail"))
	quietRail.One(t, ".rail-health")
	if banners := quietRail.Find(".rail-health.banner"); len(banners) != 0 {
		t.Errorf("banner opened with no failed tick: %s", banners[0].HTML())
	}

	failed := newServer(shellStore(t, &observedAt, "gh is unavailable"), now)
	banner := htmltest.Parse(t, renderPath(t, failed, "/rail")).One(t, ".rail-health.banner")
	for _, want := range []string{"tick failed 44s ago", "gh is unavailable"} {
		if got := banner.Text(); !strings.Contains(got, want) {
			t.Errorf("banner = %q, want %q: %s", got, want, banner.HTML())
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

	rail := htmltest.Parse(t, renderPath(t, newServer(store, now), "/rail"))
	if banners := rail.Find(".rail-health.banner"); len(banners) != 0 {
		t.Errorf("banner persists after recovery: %s", banners[0].HTML())
	}
	chip := rail.One(t, ".rail-health")
	if got := chip.Text(); !strings.Contains(got, "observed 2s ago") {
		t.Errorf("recovered chip = %q: %s", got, chip.HTML())
	}
}

func TestRailCountsLiveAgents(t *testing.T) {
	t.Parallel()

	now := testNow
	live := newServer(detailStore(t, writeLog(t, 1), now, now), now)
	chip := htmltest.Parse(t, renderPath(t, live, "/rail")).One(t, ".rail-health")
	if !strings.Contains(chip.Text(), "1 live") {
		t.Errorf("live agent chip = %q: %s", chip.Text(), chip.HTML())
	}

	idle := newServer(shellStore(t, &now, ""), now)
	chip = htmltest.Parse(t, renderPath(t, idle, "/rail")).One(t, ".rail-health")
	if got := chip.Text(); !strings.Contains(got, "0 live") {
		t.Errorf("idle chip = %q: %s", got, chip.HTML())
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
	storetest.Tickets(t, store, blocker, dependent)
	runID, err := store.InsertRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: dependent.URL, Kind: "agent", BaselineSHA: "basesha1234", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RecordSpawn(ctx, storepkg.Spawn{
		RunID: plan.RunID(runID), Pgid: 4242, StartedAt: now, LogPath: "/logs/run.jsonl",
	}); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observation{
		ObservedAt: now,
		Runs:       map[plan.TicketURL]plan.RunObservation{plan.TicketURL(dependent.URL): {Alive: true}},
		PRs:        map[plan.BranchRef]plan.PR{{Branch: "cc-1"}: {State: plan.Closed}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	chip := htmltest.Parse(t, renderPath(t, newServer(store, now), "/rail")).One(t, ".rail-health")
	if got := chip.Text(); !strings.Contains(got, "1 live") {
		t.Errorf("base_gone live chip = %q: %s", got, chip.HTML())
	}
}
