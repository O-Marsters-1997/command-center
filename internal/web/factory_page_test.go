package web_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func factoryColumn(t *testing.T, root htmltest.Node, stage string) htmltest.Node {
	t.Helper()
	return root.One(t, fmt.Sprintf(`section[data-stage="%s"]`, stage))
}

func TestFactoryPage(t *testing.T) {
	t.Parallel()

	t.Run("six columns in stage order each hold their tickets", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPath(t, newServer(seededStore(t, testNow), testNow), "/factory"))

		var got []string
		for _, col := range root.Find("section.factory-col") {
			stage, _ := col.Attr("data-stage")
			got = append(got, stage)
		}
		if diff := cmp.Diff([]string{"blocked", "ready", "working", "pr", "review", "merged"}, got); diff != "" {
			t.Errorf("GET /factory columns mismatch (-want +got):\n%s", diff)
		}
		if text := factoryColumn(t, root, "ready").Text(); !strings.Contains(text, "#CC-1") {
			t.Errorf("ready column = %q, want #CC-1", text)
		}
		blocked := factoryColumn(t, root, "blocked").Text()
		if !strings.Contains(blocked, "#CC-2") || !strings.Contains(blocked, "After #CC-1") {
			t.Errorf("blocked column = %q, want #CC-2 after #CC-1", blocked)
		}
		if text := factoryColumn(t, root, "working").Text(); !strings.Contains(text, "No agents running") {
			t.Errorf("working column = %q, want its empty line", text)
		}
	})

	t.Run("blocked caps at four and merged at ten", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		storetest.Tickets(t, st, store.Ticket{URL: "sandbox://CC-0", Repo: "cc-sandbox", Branch: "cc-0"})
		for i := 1; i <= 6; i++ {
			storetest.Tickets(t, st, store.Ticket{
				URL: fmt.Sprintf("sandbox://B-%d", i), Repo: "cc-sandbox", Branch: fmt.Sprintf("b-%d", i),
				BlockedBy: []string{"sandbox://CC-0"},
			})
		}
		prs := map[plan.BranchRef]plan.PR{}
		for i := 1; i <= 12; i++ {
			branch := fmt.Sprintf("m-%d", i)
			url := fmt.Sprintf("sandbox://M-%d", i)
			storetest.Tickets(t, st, store.Ticket{URL: url, Repo: "cc-sandbox", Branch: branch})
			storetest.Disposed(t, st, url, plan.RunKindAgent, testNow, 1)
			prs[branchRef("cc-sandbox", branch)] = plan.PR{Number: i, HeadRef: branch, State: plan.Merged, MergedAt: testNow}
		}
		if err := st.SaveObservation(t.Context(), plan.Observation{ObservedAt: testNow, PRs: prs}); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}
		server := newServer(st, testNow)

		root := htmltest.Parse(t, renderPath(t, server, "/factory"))
		blocked := factoryColumn(t, root, "blocked")
		if got := len(blocked.Find("article")); got != 4 {
			t.Errorf("blocked cards = %d, want 4", got)
		}
		more := blocked.One(t, `a[href="/factory?blocked=all"]`)
		if !strings.Contains(more.Text(), "Show 2 more") {
			t.Errorf("show more = %q, want Show 2 more", more.Text())
		}
		merged := factoryColumn(t, root, "merged")
		if got := len(merged.Find("article")); got != 10 {
			t.Errorf("merged cards = %d, want 10", got)
		}
		seeAll := merged.One(t, `a[href="/tickets?filter=done"]`)
		if !strings.Contains(seeAll.Text(), "See all 12 merged") {
			t.Errorf("see all = %q, want See all 12 merged", seeAll.Text())
		}

		all := htmltest.Parse(t, renderPath(t, server, "/factory?blocked=all"))
		blocked = factoryColumn(t, all, "blocked")
		if got := len(blocked.Find("article")); got != 6 {
			t.Errorf("GET /factory?blocked=all blocked cards = %d, want 6", got)
		}
		blocked.One(t, `a[href="/factory"]`)
		if poll, _ := all.One(t, "div#factory").Attr("hx-get"); poll != "/factory?blocked=all" {
			t.Errorf("poll path = %q, want /factory?blocked=all", poll)
		}
	})

	t.Run("scope narrows the floor and survives the poll", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		storetest.FeatureTicket(t, st, "sandbox://A-1", "cc-sandbox", "project:alpha")
		storetest.FeatureTicket(t, st, "sandbox://Z-1", "cc-sandbox", "project:zeta")

		root := htmltest.Parse(t, renderPath(t, newServer(st, testNow), "/factory?feature=project:alpha"))
		var cards []string
		for _, card := range root.Find("article.factory-card") {
			cards = append(cards, card.Text())
		}
		if len(cards) != 1 || !strings.Contains(cards[0], "#A-1") {
			t.Errorf("scoped cards = %q, want only #A-1", cards)
		}
		got := attrs(root.One(t, "div#factory"), "hx-get", "hx-trigger", "hx-select", "hx-sync", "hx-swap")
		want := map[string]string{
			"hx-get": "/factory?feature=project%3Aalpha", "hx-trigger": "every 5s", "hx-select": "#factory",
			"hx-sync": "this:drop", "hx-swap": "outerHTML",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("GET /factory poll mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("the rail links to the floor", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPath(t, newServer(seededStore(t, testNow), testNow), "/factory"))

		links := root.Find(`nav#sidebar-nav a`)
		if len(links) == 0 {
			t.Fatal("rail nav has no links")
		}
		if diff := cmp.Diff(navAttrs("/factory"), attrs(links[0], navAttrNames...)); diff != "" {
			t.Errorf("first rail link htmx attributes mismatch (-want +got):\n%s", diff)
		}
	})
}
