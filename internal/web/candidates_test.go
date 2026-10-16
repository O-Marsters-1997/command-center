package web_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

type jsonCandidate struct {
	URL         string   `json:"url"`
	Ref         string   `json:"ref"`
	Title       string   `json:"title"`
	Repo        string   `json:"repo"`
	Feature     string   `json:"feature"`
	Label       string   `json:"label"`
	Reason      string   `json:"reason"`
	Base        string   `json:"base"`
	BaseVerdict string   `json:"base_verdict"`
	PromptHash  string   `json:"prompt_hash"`
	BlockedBy   []string `json:"blocked_by"`
}

func fetchCandidates(t *testing.T, server http.Handler, query string) []jsonCandidate {
	t.Helper()

	response := do(t, server, http.MethodGet, "/launch/candidates.json?"+query, req{})
	if response.Code != http.StatusOK {
		t.Fatalf("GET /launch/candidates.json?%s status = %d, want 200", query, response.Code)
	}
	var candidates []jsonCandidate
	if err := json.Unmarshal([]byte(response.Body), &candidates); err != nil {
		t.Fatalf("decode /launch/candidates.json?%s: %v", query, err)
	}
	return candidates
}

func candidateFor(t *testing.T, candidates []jsonCandidate, ticketURL string) jsonCandidate {
	t.Helper()

	for _, c := range candidates {
		if c.URL == ticketURL {
			return c
		}
	}
	t.Fatalf("no candidate for %s in %+v", ticketURL, candidates)
	return jsonCandidate{}
}

func candidateStore(t *testing.T, tickets ...storepkg.Ticket) *storepkg.Store {
	t.Helper()
	store := openStore(t)
	storetest.Tickets(t, store, tickets...)
	if err := store.SaveObservation(t.Context(), plan.Observation{PRs: map[plan.BranchRef]plan.PR{}}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}
	return store
}

func TestCandidatesJSON(t *testing.T) {
	t.Parallel()

	t.Run("answers JSON with or without HX-Request", func(t *testing.T) {
		t.Parallel()
		srv := newServer(candidateStore(t, storepkg.Ticket{
			URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x",
		}), testNow)

		for _, tt := range []struct {
			name   string
			header http.Header
		}{
			{name: "without HX-Request"},
			{name: "with HX-Request", header: http.Header{"Hx-Request": {"true"}}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				response := do(t, srv, http.MethodGet, "/launch/candidates.json?feature=project%3Ax", req{Header: tt.header})
				if ct := response.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("GET /launch/candidates.json Content-Type = %q, want application/json", ct)
				}
			})
		}
	})

	t.Run("labels reasons, bases and blockers for a requested slice", func(t *testing.T) {
		t.Parallel()
		store := candidateStore(t,
			storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
			storepkg.Ticket{
				URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"},
			},
			storepkg.Ticket{
				URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third", BlockedBy: []string{"sandbox://CC-4"},
			},
			storepkg.Ticket{URL: "sandbox://CC-4", Repo: "cc-sandbox", Branch: "cc-4-fourth"},
		)
		candidates := fetchCandidates(t, newServer(store, testNow),
			"ticket=sandbox://CC-1&ticket=sandbox://CC-2&ticket=sandbox://CC-3")

		cc1 := candidateFor(t, candidates, "sandbox://CC-1")
		if cc1.Label != "now" || cc1.Base != "origin/main" || cc1.Ref != "#CC-1" || cc1.PromptHash == "" {
			t.Errorf("CC-1 = %+v, want label now, base origin/main, ref #CC-1 and a prompt hash", cc1)
		}
		cc2 := candidateFor(t, candidates, "sandbox://CC-2")
		if cc2.Label != "on unlock" || cc2.Base != "origin/main" {
			t.Errorf("CC-2 = %+v, want label on unlock, base origin/main", cc2)
		}
		if diff := cmp.Diff([]string{"sandbox://CC-1"}, cc2.BlockedBy); diff != "" {
			t.Errorf("CC-2 BlockedBy mismatch (-want +got):\n%s", diff)
		}
		cc3 := candidateFor(t, candidates, "sandbox://CC-3")
		if cc3.Label != "refused" || !strings.Contains(cc3.Reason, "sandbox://CC-4") {
			t.Errorf("CC-3 = %+v, want refused naming the out-of-slice blocker sandbox://CC-4", cc3)
		}
	})

	t.Run("shows the base's verdict for a stacked row", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		storetest.Tickets(t, store,
			storepkg.Ticket{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
			storepkg.Ticket{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
		)
		dispositionAsPushed(t, store, "sandbox://PARENT", testNow)
		storetest.Pushed(t, store, storetest.PushedOpts{
			URL: "sandbox://PARENT", Tip: "parent-tip", Base: "main", BaseTip: "main-tip", At: testNow,
		})
		obs := plan.Observation{
			BranchTips: map[plan.BranchRef]plan.SHA{
				branchRef("repo", "parent"): "parent-tip", branchRef("repo", plan.DefaultBaseBranch): "main-tip",
			},
			PRs: map[plan.BranchRef]plan.PR{branchRef("repo", "parent"): {
				Number: 1, State: plan.Open, HeadOid: "parent-tip",
				Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: "FAILURE"}},
			}},
			Settings: map[string]config.RepoSettings{"repo": {Stacking: true, Checks: verdict.Predicate{Success: "CI"}}},
		}
		if err := store.SaveObservation(t.Context(), obs); err != nil {
			t.Fatalf("SaveObservation: %v", err)
		}

		candidates := fetchCandidates(t, newServer(track(t, store, named("repo")...), testNow), "ticket=sandbox://CHILD")
		child := candidateFor(t, candidates, "sandbox://CHILD")
		if child.Label != "now" || child.Base != "origin/parent" || child.BaseVerdict != "ci_failed" {
			t.Errorf("CHILD = %+v, want label now, base origin/parent, base verdict ci_failed", child)
		}
	})

	t.Run("an already authorised member is refused", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		store := openStore(t)
		storetest.Tickets(t, store, storepkg.Ticket{
			URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x",
		})
		if err := store.QueueLaunchIntent(ctx, storepkg.LaunchRequest{
			Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: testNow,
		}); err != nil {
			t.Fatalf("QueueLaunchIntent(sandbox://CC-1): %v", err)
		}
		if err := store.ApplyLaunchIntents(ctx, testNow); err != nil {
			t.Fatalf("ApplyLaunchIntents: %v", err)
		}

		cc1 := candidateFor(t, fetchCandidates(t, newServer(store, testNow), "feature=project%3Ax"), "sandbox://CC-1")
		if cc1.Label != "refused" || !strings.Contains(cc1.Reason, "already authorised") {
			t.Errorf("CC-1 = %+v, want refused as already authorised", cc1)
		}
	})

	t.Run("every dependent of a mid-stack blocker outside the slice", func(t *testing.T) {
		t.Parallel()
		store := candidateStore(t,
			storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
			storepkg.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2", BlockedBy: []string{"sandbox://CC-1"}},
			storepkg.Ticket{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3", BlockedBy: []string{"sandbox://CC-2"}},
			storepkg.Ticket{URL: "sandbox://CC-4", Repo: "cc-sandbox", Branch: "cc-4", BlockedBy: []string{"sandbox://CC-2"}},
			storepkg.Ticket{URL: "sandbox://CC-5", Repo: "cc-sandbox", Branch: "cc-5", BlockedBy: []string{"sandbox://CC-3"}},
			storepkg.Ticket{URL: "sandbox://CC-6", Repo: "cc-sandbox", Branch: "cc-6", BlockedBy: []string{"sandbox://CC-4"}},
			storepkg.Ticket{URL: "sandbox://CC-7", Repo: "cc-sandbox", Branch: "cc-7", BlockedBy: []string{"sandbox://CC-5"}},
		)
		candidates := fetchCandidates(t, newServer(store, testNow),
			"ticket=sandbox://CC-3&ticket=sandbox://CC-4&ticket=sandbox://CC-5&ticket=sandbox://CC-6&ticket=sandbox://CC-7")

		got := map[string]string{}
		for _, c := range candidates {
			got[c.URL] = c.Label
			if c.Label == "refused" && !strings.Contains(c.Reason, "sandbox://CC-2") {
				t.Errorf("%s reason = %q, want it to name the out-of-slice blocker sandbox://CC-2", c.URL, c.Reason)
			}
		}
		want := map[string]string{
			"sandbox://CC-3": "refused", "sandbox://CC-4": "refused",
			"sandbox://CC-5": "on unlock", "sandbox://CC-6": "on unlock", "sandbox://CC-7": "on unlock",
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("candidate labels mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a slice spanning two features is launchable", func(t *testing.T) {
		t.Parallel()
		store := candidateStore(t,
			storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1", Feature: "project:x"},
			storepkg.Ticket{
				URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2", Feature: "project:y",
				BlockedBy: []string{"sandbox://CC-1"},
			},
			storepkg.Ticket{
				URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3", Feature: "project:x",
				BlockedBy: []string{"sandbox://CC-4"},
			},
			storepkg.Ticket{URL: "sandbox://CC-4", Repo: "cc-sandbox", Branch: "cc-4", Feature: "project:x"},
		)
		candidates := fetchCandidates(t, newServer(store, testNow),
			"ticket=sandbox://CC-1&ticket=sandbox://CC-2&ticket=sandbox://CC-3")

		got := map[string]string{}
		for _, c := range candidates {
			got[c.URL] = c.Label
		}
		want := map[string]string{"sandbox://CC-1": "now", "sandbox://CC-2": "on unlock", "sandbox://CC-3": "refused"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("candidate labels mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a feature returns every stored ticket in it", func(t *testing.T) {
		t.Parallel()
		store := candidateStore(t,
			storepkg.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "widgets"},
			storepkg.Ticket{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", Feature: "widgets"},
			storepkg.Ticket{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third", Feature: "gadgets"},
		)

		var got []string
		for _, c := range fetchCandidates(t, newServer(store, testNow), "feature=widgets") {
			got = append(got, c.URL)
		}
		if diff := cmp.Diff([]string{"sandbox://CC-1", "sandbox://CC-2"}, got); diff != "" {
			t.Errorf("GET /launch/candidates.json?feature=widgets URLs mismatch (-want +got):\n%s", diff)
		}
	})
}
