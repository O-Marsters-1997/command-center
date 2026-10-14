package web_test

import (
	"net/http"
	"strings"
	"testing"

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

	scoped := rail("http://cc/tickets?project=acme")
	if !strings.Contains(scoped, `href="/s/acme/alpha/1?project=acme"`) {
		t.Errorf("scoped rail missing the acme card with its scope:\n%s", scoped)
	}
	if strings.Contains(scoped, "/s/acme/solo/4") {
		t.Errorf("scoped rail listed a ticket outside the project:\n%s", scoped)
	}
	if unscoped := rail("http://cc/tickets"); !strings.Contains(unscoped, "/s/acme/solo/4") {
		t.Errorf("unscoped rail dropped a ticket:\n%s", unscoped)
	}
}
