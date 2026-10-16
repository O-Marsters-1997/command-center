package web_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func TestFeaturePage(t *testing.T) {
	t.Parallel()

	t.Run("sections the board and names dependencies", func(t *testing.T) {
		t.Parallel()
		page := renderPath(t, threeFeatureServer(t), "/f/board-scope")

		board := htmltest.Parse(t, page).One(t, "div#board")
		var keys []string
		for _, section := range board.Find("section[data-section]") {
			key, _ := section.Attr("data-section")
			keys = append(keys, key)
		}
		if got := strings.Join(keys, ","); got != "ready,blocked" {
			t.Errorf("GET /f/board-scope sections = %q, want ready,blocked", got)
		}
		if got := len(board.Find("a[data-ref]")); got != 2 {
			t.Errorf("GET /f/board-scope rows = %d, want 2", got)
		}
		for _, control := range []string{"input", "button", "form"} {
			if found := board.Find(control); len(found) != 0 {
				t.Errorf("GET /f/board-scope board has a %s: %s", control, found[0].HTML())
			}
		}
	})

	t.Run("the top bar offers Launch with the count of tickets to launch", func(t *testing.T) {
		t.Parallel()
		main := htmltest.Parse(t, renderPath(t, threeFeatureServer(t), "/f/board-scope")).One(t, "main#main")
		if got := main.One(t, `a[href="/f/board-scope/launch"]`).Text(); !strings.Contains(got, "Launch · 2") {
			t.Errorf("GET /f/board-scope launch button = %q, want Launch · 2", got)
		}
	})

	t.Run("the tab pair marks the board current", func(t *testing.T) {
		t.Parallel()
		main := htmltest.Parse(t, renderPath(t, threeFeatureServer(t), "/f/board-scope")).One(t, "main#main")

		for _, tt := range []struct{ href, current string }{
			{"/f/board-scope", "page"}, {"/f/board-scope/graph", ""},
		} {
			link := main.One(t, `nav[aria-label="feature views"] a[href="`+tt.href+`"]`)
			want := navAttrs(tt.href)
			want["aria-current"] = tt.current
			if diff := cmp.Diff(want, attrs(link, append(navAttrNames, "aria-current")...)); diff != "" {
				t.Errorf("GET /f/board-scope tab %s mismatch (-want +got):\n%s", tt.href, diff)
			}
		}
		main.One(t, `a[href="/f/board-scope/launch"][data-key="c"]`)
	})

	t.Run("an unknown feature is 404", func(t *testing.T) {
		t.Parallel()
		if rec := do(t, threeFeatureServer(t), http.MethodGet, "/f/bogus", req{}); rec.Code != http.StatusNotFound {
			t.Errorf("GET /f/bogus status = %d, want 404", rec.Code)
		}
	})

	t.Run("the header names the launch, tickets and waves", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := threeFeatureStore(t)
		launchedAt := testNow.Add(-2 * time.Hour)
		if err := store.QueueLaunchIntent(ctx, storepkg.LaunchRequest{
			Ticket: "sandbox://ROOT", PromptHash: "hash-1", Group: "group-a", At: launchedAt,
		}); err != nil {
			t.Fatalf("QueueLaunchIntent(sandbox://ROOT): %v", err)
		}
		if err := store.ApplyLaunchIntents(ctx, launchedAt); err != nil {
			t.Fatalf("ApplyLaunchIntents: %v", err)
		}
		server := newServer(store, testNow)

		launched := htmltest.Parse(t, renderPath(t, server, "/f/board-scope")).One(t, "main#main")
		stamp := launchedAt.UTC().Format(time.RFC3339)
		if got := launched.One(t, `time[datetime="`+stamp+`"]`).Text(); got != "2h0m0s ago" {
			t.Errorf("GET /f/board-scope launch time = %q, want 2h0m0s ago", got)
		}
		if got := launched.One(t, "header.feature-head").Text(); !strings.Contains(got, "2 tickets in 2 waves") {
			t.Errorf("GET /f/board-scope header = %q, want 2 tickets in 2 waves", got)
		}

		unlaunched := htmltest.Parse(t, renderPath(t, server, "/f/sqlc-migration")).One(t, "main#main")
		if times := unlaunched.Find("time[datetime]"); len(times) != 0 {
			t.Errorf("GET /f/sqlc-migration has a launch time: %s", times[0].HTML())
		}
		if got := unlaunched.One(t, "header.feature-head").Text(); !strings.Contains(got, "1 ticket in 1 wave") {
			t.Errorf("GET /f/sqlc-migration header = %q, want 1 ticket in 1 wave", got)
		}
	})
}
