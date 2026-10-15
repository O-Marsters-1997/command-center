package web_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
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
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if ct := response.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var candidates []jsonCandidate
	if err := json.Unmarshal([]byte(response.Body), &candidates); err != nil {
		t.Fatalf("decode /launch/candidates.json: %v", err)
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

func TestCandidatesLabelsReasonsBasesAndBlockedByForARequestedSlice(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
		{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third", BlockedBy: []string{"sandbox://CC-4"}},
		{URL: "sandbox://CC-4", Repo: "cc-sandbox", Branch: "cc-4-fourth"},
	}
	storetest.Tickets(t, store, tickets...)
	if err := store.SaveObservation(ctx, plan.Observation{PRs: map[plan.BranchRef]plan.PR{}}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	query := "ticket=sandbox://CC-1&ticket=sandbox://CC-2&ticket=sandbox://CC-3"
	candidates := fetchCandidates(t, newServer(store, testNow), query)

	cc1 := candidateFor(t, candidates, "sandbox://CC-1")
	if cc1.Label != "now" || cc1.Base != "origin/main" {
		t.Errorf("CC-1 = %+v, want label now, base origin/main", cc1)
	}
	if cc1.PromptHash == "" {
		t.Errorf("CC-1 PromptHash is empty")
	}
	if cc1.Ref != "#CC-1" {
		t.Errorf("CC-1 Ref = %q, want #CC-1", cc1.Ref)
	}

	cc2 := candidateFor(t, candidates, "sandbox://CC-2")
	if cc2.Label != "on unlock" || cc2.Base != "origin/main" {
		t.Errorf("CC-2 = %+v, want label on unlock, base origin/main", cc2)
	}
	if len(cc2.BlockedBy) != 1 || cc2.BlockedBy[0] != "sandbox://CC-1" {
		t.Errorf("CC-2 BlockedBy = %v, want [sandbox://CC-1]", cc2.BlockedBy)
	}

	cc3 := candidateFor(t, candidates, "sandbox://CC-3")
	if cc3.Label != "refused" {
		t.Errorf("CC-3 Label = %q, want refused", cc3.Label)
	}
	if !strings.Contains(cc3.Reason, "sandbox://CC-4") {
		t.Errorf("CC-3 Reason = %q, want it to name the out-of-slice blocker", cc3.Reason)
	}
}

func TestCandidatesShowsTheBasesVerdictForAStackedRow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	storetest.Tickets(t, store, tickets...)

	at := testNow
	dispositionAsPushed(t, store, "sandbox://PARENT", at)
	const parentTip = "parent-tip"
	storetest.Pushed(t, store, storetest.PushedOpts{
		URL: "sandbox://PARENT", Tip: parentTip, Base: "main", BaseTip: "main-tip", At: at,
	})

	obs := plan.Observation{
		BranchTips: map[plan.BranchRef]plan.SHA{
			branchRef("repo", "parent"): plan.SHA(parentTip), branchRef("repo", plan.DefaultBaseBranch): "main-tip",
		},
		PRs: map[plan.BranchRef]plan.PR{
			branchRef("repo", "parent"): {
				Number: 1, State: plan.Open, HeadOid: parentTip,
				Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: "FAILURE"}},
			},
		},
		Settings: map[string]config.RepoSettings{"repo": {Stacking: true, Checks: verdict.Predicate{Success: "CI"}}},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	repos := named("repo")
	candidates := fetchCandidates(t, newServer(track(t, store, repos...), at), "ticket=sandbox://CHILD")

	child := candidateFor(t, candidates, "sandbox://CHILD")
	if child.Label != "now" || child.Base != "origin/parent" || child.BaseVerdict != "ci_failed" {
		t.Errorf("CHILD = %+v, want label now, base origin/parent, base verdict ci_failed", child)
	}
}

func TestCandidatesShowsAnAlreadyAuthorisedMemberAsRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	storetest.Tickets(t, store, storepkg.Ticket{
		URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x",
	})
	at := testNow
	if err := store.QueueLaunchIntent(ctx, storepkg.LaunchRequest{
		Ticket: "sandbox://CC-1", PromptHash: "hash-1", Group: "group-a", At: at,
	}); err != nil {
		t.Fatalf("QueueLaunchIntent(sandbox://CC-1): %v", err)
	}
	if err := store.ApplyLaunchIntents(ctx, at); err != nil {
		t.Fatalf("ApplyLaunchIntents: %v", err)
	}

	candidates := fetchCandidates(t, newServer(store, testNow), "feature=project%3Ax")

	cc1 := candidateFor(t, candidates, "sandbox://CC-1")
	if cc1.Label != "refused" || !strings.Contains(cc1.Reason, "already authorised in launch") {
		t.Errorf("CC-1 = %+v, want label refused naming the launch", cc1)
	}
}

func TestCandidatesRefusesEveryDependentOfAMidStackBlockerOutsideTheSlice(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2", BlockedBy: []string{"sandbox://CC-1"}},
		{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3", BlockedBy: []string{"sandbox://CC-2"}},
		{URL: "sandbox://CC-4", Repo: "cc-sandbox", Branch: "cc-4", BlockedBy: []string{"sandbox://CC-2"}},
		{URL: "sandbox://CC-5", Repo: "cc-sandbox", Branch: "cc-5", BlockedBy: []string{"sandbox://CC-3"}},
		{URL: "sandbox://CC-6", Repo: "cc-sandbox", Branch: "cc-6", BlockedBy: []string{"sandbox://CC-4"}},
		{URL: "sandbox://CC-7", Repo: "cc-sandbox", Branch: "cc-7", BlockedBy: []string{"sandbox://CC-5"}},
	}
	storetest.Tickets(t, store, tickets...)
	if err := store.SaveObservation(ctx, plan.Observation{PRs: map[plan.BranchRef]plan.PR{}}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	candidates := fetchCandidates(t, newServer(store, testNow),
		"ticket=sandbox://CC-3&ticket=sandbox://CC-4&ticket=sandbox://CC-5&ticket=sandbox://CC-6&ticket=sandbox://CC-7")

	for _, ticketURL := range []string{"sandbox://CC-3", "sandbox://CC-4"} {
		c := candidateFor(t, candidates, ticketURL)
		if c.Label != "refused" || !strings.Contains(c.Reason, "sandbox://CC-2") {
			t.Errorf("%s = %+v, want refused naming the out-of-slice blocker", ticketURL, c)
		}
	}
	for _, ticketURL := range []string{"sandbox://CC-5", "sandbox://CC-6", "sandbox://CC-7"} {
		if c := candidateFor(t, candidates, ticketURL); c.Label != "on unlock" {
			t.Errorf("%s Label = %q, want on unlock", ticketURL, c.Label)
		}
	}
}

func TestCandidatesSpanningTwoFeaturesIsLaunchableWithAnOutOfSliceBlockerRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1", Feature: "project:x"},
		{
			URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2", Feature: "project:y",
			BlockedBy: []string{"sandbox://CC-1"},
		},
		{
			URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3", Feature: "project:x",
			BlockedBy: []string{"sandbox://CC-4"},
		},
		{URL: "sandbox://CC-4", Repo: "cc-sandbox", Branch: "cc-4", Feature: "project:x"},
	}
	storetest.Tickets(t, store, tickets...)
	if err := store.SaveObservation(ctx, plan.Observation{PRs: map[plan.BranchRef]plan.PR{}}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	query := "ticket=sandbox://CC-1&ticket=sandbox://CC-2&ticket=sandbox://CC-3"
	candidates := fetchCandidates(t, newServer(store, testNow), query)

	if cc1 := candidateFor(t, candidates, "sandbox://CC-1"); cc1.Label != "now" {
		t.Errorf("CC-1 (project:x) Label = %q, want now", cc1.Label)
	}
	if cc2 := candidateFor(t, candidates, "sandbox://CC-2"); cc2.Label != "on unlock" {
		t.Errorf("CC-2 (project:y, blocked by project:x's CC-1) Label = %q, want on unlock", cc2.Label)
	}
	cc3 := candidateFor(t, candidates, "sandbox://CC-3")
	if cc3.Label != "refused" || !strings.Contains(cc3.Reason, "sandbox://CC-4") {
		t.Errorf("CC-3 = %+v, want refused naming the out-of-slice blocker sandbox://CC-4", cc3)
	}
}

func TestCandidatesByFeatureReturnsEveryStoredTicketInIt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "widgets"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", Feature: "widgets"},
		{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third", Feature: "gadgets"},
	}
	storetest.Tickets(t, store, tickets...)
	if err := store.SaveObservation(ctx, plan.Observation{PRs: map[plan.BranchRef]plan.PR{}}); err != nil {
		t.Fatalf("SaveObservation: %v", err)
	}

	candidates := fetchCandidates(t, newServer(store, testNow), "feature=widgets")

	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2: %+v", len(candidates), candidates)
	}
	candidateFor(t, candidates, "sandbox://CC-1")
	candidateFor(t, candidates, "sandbox://CC-2")
}
