package web_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestFeaturePageSectionsTheBoardAndNamesDependencies(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope")

	if strings.Contains(page, ticketRef("sandbox://LONE")) {
		t.Errorf("/f/board-scope rendered a ticket from another feature:\n%s", page)
	}
	order := regexp.MustCompile(`data-section="([a-z-]+)"`).FindAllStringSubmatch(page, -1)
	var got []string
	for _, m := range order {
		got = append(got, m[1])
	}
	if want := "ready,blocked"; strings.Join(got, ",") != want {
		t.Errorf("sections = %v, want %s", got, want)
	}
	if cell := rowCellAt(t, page, "sandbox://CHILD", 3); !strings.Contains(cell, "after #ROOT") {
		t.Errorf("CHILD ticket cell = %q, want it to read after #ROOT", cell)
	}
	if cell := rowCellAt(t, page, "sandbox://ROOT", 3); !strings.Contains(cell, "unlocks #CHILD") {
		t.Errorf("ROOT ticket cell = %q, want it to read unlocks #CHILD", cell)
	}
}

func TestFeaturePageTabPairMarksTheBoardCurrent(t *testing.T) {
	t.Parallel()

	page := renderPath(t, threeFeatureServer(t), "/f/board-scope")

	for _, want := range []string{
		`<a class="pill-tab" href="/f/board-scope" aria-current="page">Board</a>`,
		`<a class="pill-tab" href="/f/board-scope/graph" data-key="v">Graph</a>`,
		`href="/f/board-scope/launch" data-key="c"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("feature page missing %s:\n%s", want, page)
		}
	}
}

func TestFeaturePageUnknownFeatureIs404(t *testing.T) {
	t.Parallel()

	rec := get(t, threeFeatureServer(t), "/f/bogus")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /f/bogus: status = %d, want 404", rec.Code)
	}
}

func TestFeaturePageHeaderNamesLaunchTicketsAndWaves(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := threeFeatureStore(t)
	launchedAt := testNow.Add(-2 * time.Hour)
	if err := store.QueueLaunchIntent(ctx, "sandbox://ROOT", "hash-1", "group-a", launchedAt); err != nil {
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
