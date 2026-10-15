package web_test

import (
	"net/http"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

func TestRailNarrowsToTheProjectInTheCurrentURL(t *testing.T) {
	t.Parallel()

	st := track(t, openStore(t), alphaRepo, soloRepo)
	alpha := "https://github.com/acme/alpha/issues/1"
	solo := "https://github.com/acme/solo/issues/4"
	tickets := []storepkg.Ticket{
		{URL: alpha, Repo: "acme/alpha", Branch: "a-1"},
		{URL: solo, Repo: "acme/solo", Branch: "s-4"},
	}
	storetest.Tickets(t, st, tickets...)
	if err := st.CreateProject(t.Context(), storepkg.NewProject{
		Name: "acme", Hue: 1, Repos: []string{"acme/alpha"}, At: testNow,
	}); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observation{ObservedAt: testNow, Runs: map[plan.TicketURL]plan.RunObservation{}}
	for i, u := range []string{alpha, solo} {
		runID, err := st.InsertRunSkeleton(t.Context(), storepkg.RunSkeleton{Ticket: u, Kind: "agent", PromptHash: "hash"})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.RecordSpawn(t.Context(), storepkg.Spawn{
			RunID: plan.RunID(runID), Pgid: 100 + i, StartedAt: testNow, LogPath: "/state/runs/x.jsonl",
		}); err != nil {
			t.Fatal(err)
		}
		obs.Runs[plan.TicketURL(u)] = plan.RunObservation{Alive: true}
	}
	if err := st.SaveObservation(t.Context(), obs); err != nil {
		t.Fatal(err)
	}
	server := newServer(st, testNow)

	rail := func(currentURL string) string {
		return do(t, server, http.MethodGet, "/rail", req{Header: http.Header{
			"Hx-Request":     {"true"},
			"Hx-Current-Url": {currentURL},
		}}).Body
	}

	scoped := htmltest.Parse(t, rail("http://cc/tickets?project=acme"))
	scopedGroup := scoped.One(t, "#rail-body").One(t, `[data-group="in-flight"]`)
	if got := len(scopedGroup.Find(`a[href="/s/acme/alpha/1?project=acme"]`)); got != 1 {
		t.Errorf("scoped rail alpha links = %d, want 1", got)
	}
	if got := len(scopedGroup.Find("a.rail-link")); got != 1 {
		t.Errorf("scoped rail ticket links = %d, want 1", got)
	}
	unscoped := htmltest.Parse(t, rail("http://cc/tickets")).One(t, "#rail-body").One(t, `[data-group="in-flight"]`)
	if got := len(unscoped.Find(`a[href="/s/acme/alpha/1"]`)); got != 1 {
		t.Errorf("unscoped rail alpha links = %d, want 1", got)
	}
	if got := len(unscoped.Find(`a[href="/s/acme/solo/4"]`)); got != 1 {
		t.Errorf("unscoped rail solo links = %d, want 1", got)
	}
	if got := len(unscoped.Find("a.rail-link")); got != 2 {
		t.Errorf("unscoped rail ticket links = %d, want 2", got)
	}
}
