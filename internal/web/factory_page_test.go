package web_test

import (
	"fmt"
	"net/http"
	"net/url"
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
			"hx-sync": "this:drop", "hx-swap": "outerHTML transition:true",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("GET /factory poll mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("lanes split the floor, fold through the url and keep their cards", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		storetest.FeatureTicket(t, st, "sandbox://A-1", "cc-sandbox", "project:alpha")
		storetest.FeatureTicket(t, st, "sandbox://A-2", "cc-sandbox", "project:alpha")
		storetest.FeatureTicket(t, st, "sandbox://Z-1", "cc-sandbox", "project:zeta")
		server := newServer(st, testNow)

		root := htmltest.Parse(t, renderPath(t, server, "/factory?by=feature"))
		if got := len(root.Find("article.factory-card")); got != 3 {
			t.Errorf("grouped cards = %d, want the 3 of the ungrouped floor", got)
		}
		heads := root.Find("a.factory-lane-head")
		if len(heads) != 2 {
			t.Fatalf("lane heads = %d, want 2", len(heads))
		}
		if diff := cmp.Diff(
			map[string]string{"aria-expanded": "true", "href": "/factory?by=feature&closed=f%3Aproject%3Aalpha"},
			attrs(heads[0], "aria-expanded", "href"),
		); diff != "" {
			t.Errorf("first lane head mismatch (-want +got):\n%s", diff)
		}

		folded := htmltest.Parse(t, renderPath(t, server, "/factory?by=feature&closed=f:project:alpha"))
		if got := len(folded.Find("article.factory-card")); got != 1 {
			t.Errorf("cards with alpha folded = %d, want 1", got)
		}
		head := folded.Find("a.factory-lane-head")[0]
		if expanded, _ := head.Attr("aria-expanded"); expanded != "false" {
			t.Errorf("folded lane aria-expanded = %q, want false", expanded)
		}
		if poll, _ := folded.One(t, "div#factory").Attr("hx-get"); poll != "/factory?by=feature&closed=f%3Aproject%3Aalpha" {
			t.Errorf("poll path = %q, want the fold kept", poll)
		}
		got := map[string]string{}
		for _, a := range folded.Find("a.btn-ghost") {
			href, _ := a.Attr("href")
			got[strings.TrimSpace(a.Text())] = href
		}
		want := map[string]string{
			"Expand all":   "/factory?by=feature",
			"Collapse all": "/factory?by=feature&closed=f%3Aproject%3Aalpha&closed=f%3Aproject%3Azeta",
		}
		for text := range got {
			if _, ok := want[text]; !ok {
				delete(got, text)
			}
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("fold-all links mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a card's verb queues the same intent as the session verb", func(t *testing.T) {
		t.Parallel()
		st := seededRunning(t)
		server := newServer(st, testNow)

		root := htmltest.Parse(t, renderPath(t, server, "/factory"))
		form := factoryColumn(t, root, "working").One(t, `form[action="/verb"]`)
		fields := url.Values{}
		for _, in := range form.Find("input") {
			name, _ := in.Attr("name")
			value, _ := in.Attr("value")
			fields.Set(name, value)
		}
		fields.Set("verb", "kill")
		got := do(t, server, http.MethodPost, "/verb", req{Form: fields})
		if got.Code != http.StatusSeeOther || got.Location != "/factory" {
			t.Fatalf("POST /verb from the floor = %d %q, want 303 /factory", got.Code, got.Location)
		}
		if diff := cmp.Diff([]string{"sandbox://CC-1 "}, pendingVerbs(t, st, "kill")); diff != "" {
			t.Errorf("pending kill intents mismatch (-want +got):\n%s", diff)
		}
		queued := factoryColumn(t, htmltest.Parse(t, renderPath(t, server, "/factory")), "working")
		if text := queued.Text(); !strings.Contains(text, "queued") {
			t.Errorf("working column after kill = %q, want queued", text)
		}
	})

	t.Run("ready cards launch and blocked cards arm", func(t *testing.T) {
		t.Parallel()
		root := htmltest.Parse(t, renderPath(t, newServer(seededStore(t, testNow), testNow), "/factory"))

		if got := factoryColumn(t, root, "ready").One(t, `form[action="/launch"] button`).Text(); got != "Launch" {
			t.Errorf("ready card button = %q, want Launch", got)
		}
		if got := factoryColumn(t, root, "blocked").One(t, `form[action="/launch"] button`).Text(); got != "Arm" {
			t.Errorf("blocked card button = %q, want Arm", got)
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
