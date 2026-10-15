package web_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestFeaturePageSectionsTheBoardAndNamesDependencies(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope")

	board := htmltest.Parse(t, page).One(t, "table#board")
	if lone := board.Find(`input[name="ticket"][value="sandbox://LONE"]`); len(lone) != 0 {
		t.Errorf("board includes another feature's ticket: %s", lone[0].HTML())
	}
	if got := sectionKeys(t, page); got != "ready,blocked" {
		t.Errorf("sections = %s, want ready,blocked", got)
	}
	child := webtest.RowFor(t, page, "sandbox://CHILD")
	if cell := child.Ticket.Text(); !strings.Contains(cell, "after #ROOT") {
		t.Errorf("CHILD ticket cell = %q, want after #ROOT:\n%s", cell, child.Row.HTML())
	}
	root := webtest.RowFor(t, page, "sandbox://ROOT")
	if cell := root.Ticket.Text(); !strings.Contains(cell, "unlocks #CHILD") {
		t.Errorf("ROOT ticket cell = %q, want unlocks #CHILD:\n%s", cell, root.Row.HTML())
	}
}

func TestFeaturePageTabPairMarksTheBoardCurrent(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope")

	main := htmltest.Parse(t, page).One(t, "main#main")
	for _, tt := range []struct{ href, current string }{
		{"/f/board-scope", "page"}, {"/f/board-scope/graph", ""},
	} {
		link := main.One(t, `nav[aria-label="feature views"] a[href="`+tt.href+`"]`)
		attrs := map[string]string{
			"hx-get": tt.href, "aria-current": tt.current, "hx-select": "#main",
			"hx-target": "#main", "hx-push-url": "true",
		}
		for name, want := range attrs {
			if got, _ := link.Attr(name); got != want {
				t.Errorf("%s %s = %q, want %q: %s", tt.href, name, got, want, link.HTML())
			}
		}
	}
	main.One(t, `a[href="/f/board-scope/launch"][data-key="c"]`)
}

func TestFeaturePageUnknownFeatureIs404(t *testing.T) {
	t.Parallel()

	rec := do(t, threeFeatureServer(t), http.MethodGet, "/f/bogus", req{})
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /f/bogus: status = %d, want 404", rec.Code)
	}
}

func TestFeaturePageHeaderNamesLaunchTicketsAndWaves(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := threeFeatureStore(t)
	launchedAt := testNow.Add(-2 * time.Hour)
	if err := store.QueueLaunchIntent(ctx, storepkg.LaunchRequest{
		Ticket: "sandbox://ROOT", PromptHash: "hash-1", Group: "group-a", At: launchedAt,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, launchedAt); err != nil {
		t.Fatal(err)
	}
	server := newServer(store, testNow)

	launched := htmltest.Parse(t, renderPath(t, server, "/f/board-scope")).One(t, "main#main")
	stamp := launchedAt.UTC().Format(time.RFC3339)
	date := launched.One(t, `time[datetime="`+stamp+`"]`)
	if got := date.Text(); got != "2h0m0s ago" {
		t.Errorf("launch time = %q: %s", got, date.HTML())
	}
	if got := launched.One(t, "header.feature-head").Text(); !strings.Contains(got, "2 tickets in 2 waves") {
		t.Errorf("feature header = %q", got)
	}

	unlaunched := htmltest.Parse(t, renderPath(t, server, "/f/sqlc-migration")).One(t, "main#main")
	if times := unlaunched.Find("time[datetime]"); len(times) != 0 {
		t.Errorf("unlaunched feature has launch time: %s", times[0].HTML())
	}
	if got := unlaunched.One(t, "header.feature-head").Text(); !strings.Contains(got, "1 ticket in 1 wave") {
		t.Errorf("unlaunched feature header = %q", got)
	}
}
