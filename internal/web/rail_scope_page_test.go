package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
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
	if err := st.UpsertTickets(t.Context(), tickets); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateProject(t.Context(), "acme", 1, []string{"acme/alpha"}, testNow); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observation{ObservedAt: testNow, Runs: map[string]plan.RunObservation{}}
	for i, u := range []string{alpha, solo} {
		runID, err := st.InsertRunSkeleton(t.Context(), u, "agent", "", "hash")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.RecordSpawn(t.Context(), runID, 100+i, testNow, "/state/runs/x.jsonl"); err != nil {
			t.Fatal(err)
		}
		obs.Runs[u] = plan.RunObservation{Alive: true}
	}
	if err := st.SaveObservation(t.Context(), obs); err != nil {
		t.Fatal(err)
	}
	server := newServer(st, testNow)

	rail := func(currentURL string) string {
		req := httptest.NewRequest(http.MethodGet, "/rail", nil)
		req.Header.Set("HX-Current-URL", currentURL)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		return rec.Body.String()
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
