package web_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func shellStore(t *testing.T, observedAt *time.Time, tickErr string) *storepkg.Store {
	t.Helper()

	ctx := t.Context()
	store := openStore(t)
	ticket := storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{ticket}); err != nil {
		t.Fatalf("UpsertTickets: %v", err)
	}
	if observedAt != nil {
		obs := plan.Observation{ObservedAt: *observedAt, PRs: map[plan.BranchRef]plan.PR{}}
		if err := store.SaveObservation(ctx, obs); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}
	}
	if tickErr != "" {
		if observedAt == nil {
			t.Fatal("a tick error is recorded against the observation's own instant, so it needs one")
		}
		tickError := storepkg.TickError{At: observedAt.Add(time.Second), Message: tickErr}
		if err := store.RecordTickError(ctx, tickError); err != nil {
			t.Fatalf("RecordTickError: %v", err)
		}
	}
	return store
}

func TestLayout(t *testing.T) {
	t.Parallel()
	now := testNow

	t.Run("title and current nav follow the page", testLayoutTitleAndCurrentNav)
	t.Run("the sidebar swaps out of band", testLayoutSidebarOOB)
	t.Run("scope menu", testLayoutScopeMenu)
	t.Run("the spend limit notice", testLayoutSpendLimitNotice)

	t.Run("is a well-formed document naming the workspace", func(t *testing.T) {
		t.Parallel()
		body := renderPath(t, openServer(shellStore(t, &now, ""), fixedClock(now), "/data/fleet-hq"), "/tickets")

		if !strings.HasPrefix(strings.ToLower(body), "<!doctype html>") {
			t.Errorf("GET /tickets does not open a document: %q", body[:min(len(body), 40)])
		}
		root := htmltest.Parse(t, body)
		if lang, _ := root.One(t, "html").Attr("lang"); lang != "en" {
			t.Errorf("GET /tickets html lang = %q, want en", lang)
		}
		if got := root.One(t, "head title").Text(); !strings.Contains(got, "Tickets") {
			t.Errorf("GET /tickets title = %q, want it to name Tickets", got)
		}
		if got := root.One(t, "body").Text(); !strings.Contains(got, "fleet-hq") {
			t.Errorf("GET /tickets body lacks the workspace name fleet-hq")
		}
	})

	for _, path := range []string{"/tickets", "/insights", "/repos"} {
		t.Run(path+" renders the full layout around main", func(t *testing.T) {
			t.Parallel()
			root := htmltest.Parse(t, renderPath(t, newServer(shellStore(t, &now, ""), now), path))
			for _, selector := range []string{"aside#sidebar", "main#main", "ol.crumbs", ".topbar-action"} {
				root.One(t, selector)
			}
		})
	}

	t.Run("a sidebar link swaps main", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPath(t, newServer(shellStore(t, &now, ""), now), "/insights"))
		link := root.One(t, `nav#sidebar-nav a[href="/insights"]`)

		want := map[string]string{"hx-get": "/insights", "hx-select": "#main", "hx-target": "#main", "hx-push-url": "true"}
		if diff := cmp.Diff(want, attrs(link, "hx-get", "hx-select", "hx-target", "hx-push-url")); diff != "" {
			t.Errorf("GET /insights sidebar link mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("the sidebar omits feature links when no ticket carries a feature", func(t *testing.T) {
		t.Parallel()
		sidebar := htmltest.Parse(t, renderPath(t, seededServer(t), "/tickets")).One(t, "aside#sidebar")
		for _, link := range sidebar.Find("a[href]") {
			if href, _ := link.Attr("href"); strings.HasPrefix(href, "/f/") {
				t.Errorf("GET /tickets sidebar links a feature: %s", link.HTML())
			}
		}
	})

	for _, path := range []string{"/", "/tickets", "/insights", "/repos"} {
		t.Run(path+" carries the shortcut sheet outside main", func(t *testing.T) {
			t.Parallel()
			root := htmltest.Parse(t, renderPath(t, newServer(shellStore(t, &now, ""), now), path))
			root.One(t, "main#main")
			sidebar := root.One(t, "aside#sidebar")
			sidebar.One(t, `button[data-key="?"]`)
			sheet := sidebar.One(t, "dialog#key-sheet")
			var got, want []string
			for _, row := range sheet.Find(".key-row") {
				got = append(got, row.One(t, "kbd").Text()+" "+row.One(t, "dd").Text())
			}
			for _, group := range view.Keymap {
				if !strings.Contains(sheet.Text(), group.Title) {
					t.Errorf("GET %s shortcut sheet lacks heading %q", path, group.Title)
				}
				for _, row := range group.Rows {
					want = append(want, row.Keys+" "+row.Does)
				}
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("GET %s shortcut rows mismatch (-want +got):\n%s", path, diff)
			}
		})
	}

	t.Run("loads htmx, the SSE extension and the glue from the binary", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPage(t, newServer(seededStore(t, testNow), testNow)))
		root.One(t, `script[src="/assets/htmx.min.js"]`)
		root.One(t, `script[src="/assets/sse.min.js"]`)
		root.One(t, `script[type="module"][src="/assets/dist/glue.js"]`)
	})

	t.Run("links the built stylesheet and inlines none", func(t *testing.T) {
		t.Parallel()
		head := htmltest.Parse(t, renderPage(t, newServer(seededStore(t, testNow), testNow))).One(t, "head")
		head.One(t, `link[rel="stylesheet"][href="/assets/app.css"]`)
		if styles := head.Find("style"); len(styles) != 0 {
			t.Errorf("GET /tickets inlines a stylesheet: %s", styles[0].HTML())
		}
	})

	t.Run("swaps refusal responses into the flash", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPage(t, newServer(openStore(t), testNow)))
		if got, _ := root.One(t, "#flash").Attr("aria-live"); got != "polite" {
			t.Errorf("GET /tickets #flash aria-live = %q, want polite", got)
		}
		if config, _ := root.One(t, `meta[name="htmx-config"]`).Attr("content"); !strings.Contains(config, `"code":"422"`) {
			t.Errorf("GET /tickets htmx-config = %q, want it to swap 422 responses", config)
		}
	})
}
