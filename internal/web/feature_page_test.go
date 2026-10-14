package web_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/web/webtest"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestFeaturePageSectionsTheBoardAndNamesDependencies(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope")

	if strings.Contains(page, ticketRef("sandbox://LONE")) {
		t.Errorf("/f/board-scope rendered a ticket from another feature:\n%s", page)
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

const navBundleTail = ` hx-select="#main" hx-target="#main" hx-swap="outerHTML" hx-push-url="true"`

func TestFeaturePageTabPairMarksTheBoardCurrent(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope")

	for _, want := range []string{
		`<a class="pill-tab" href="/f/board-scope" hx-get="/f/board-scope"` + navBundleTail +
			` aria-current="page">Board</a>`,
		`<a class="pill-tab" href="/f/board-scope/graph" data-key="v" hx-get="/f/board-scope/graph"` +
			navBundleTail + `>Graph</a>`,
		`href="/f/board-scope/launch" data-key="c"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("feature page missing %s:\n%s", want, page)
		}
	}
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

	launched := renderPath(t, server, "/f/board-scope")
	stamp := launchedAt.UTC().Format(time.RFC3339)
	want := `launched <time datetime="` + stamp + `">2h0m0s ago</time> · 2 tickets in 2 waves`
	if !strings.Contains(launched, want) {
		t.Errorf("launched feature header missing %q:\n%s", want, launched)
	}

	unlaunched := renderPath(t, server, "/f/sqlc-migration")
	if strings.Contains(unlaunched, "<p>launched") || strings.Contains(unlaunched, " · launched ") {
		t.Errorf("a feature no launch holds should not read launched:\n%s", unlaunched)
	}
	if want := "1 ticket in 1 wave"; !strings.Contains(unlaunched, want) {
		t.Errorf("unlaunched feature header missing %q:\n%s", want, unlaunched)
	}
}
