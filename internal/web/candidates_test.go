package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"

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

func fetchCandidates(t *testing.T, srv *httptest.Server, query string) []jsonCandidate {
	t.Helper()

	resp, err := http.Get(srv.URL + "/launch/candidates?" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var candidates []jsonCandidate
	if err := json.NewDecoder(resp.Body).Decode(&candidates); err != nil {
		t.Fatalf("decode /launch/candidates: %v", err)
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
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, plan.Observation{PRs: map[string]plan.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(web.NewServer(store, realClock{}, nil, ""))
	t.Cleanup(srv.Close)

	candidates := fetchCandidates(t, srv, "ticket=sandbox://CC-1&ticket=sandbox://CC-2&ticket=sandbox://CC-3")

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
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}

	at := testNow
	dispositionAsPushed(t, store, "sandbox://PARENT", at)
	const parentTip = "parent-tip"
	if err := store.RecordPush(ctx, "sandbox://PARENT", parentTip, "main", "main-tip", at); err != nil {
		t.Fatal(err)
	}

	obs := plan.Observation{
		BranchTips: map[string]string{plan.BranchKey("repo", "parent"): parentTip, web.MainTipKey("repo"): "main-tip"},
		PRs: map[string]plan.PR{
			plan.BranchKey("repo", "parent"): {
				Number: 1, State: plan.Open, HeadOid: parentTip,
				Checks: map[string]plan.CheckState{"CI": {Status: "COMPLETED", Conclusion: "FAILURE"}},
			},
		},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	repos := []config.Repo{{Name: "repo", Stacking: true, Checks: verdict.Predicate{Success: "CI"}}}
	srv := httptest.NewServer(web.NewServer(store, fixedClock(at), repos, ""))
	t.Cleanup(srv.Close)

	candidates := fetchCandidates(t, srv, "ticket=sandbox://CHILD")

	child := candidateFor(t, candidates, "sandbox://CHILD")
	if child.Label != "now" || child.Base != "origin/parent" || child.BaseVerdict != "ci_failed" {
		t.Errorf("CHILD = %+v, want label now, base origin/parent, base verdict ci_failed", child)
	}
}

func TestCandidatesShowsAnAlreadyAuthorisedMemberAsRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "project:x"},
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	if err := store.QueueLaunchIntent(ctx, "sandbox://CC-1", "hash-1", "group-a", at); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyLaunchIntents(ctx, at); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(web.NewServer(store, realClock{}, nil, ""))
	t.Cleanup(srv.Close)

	candidates := fetchCandidates(t, srv, "feature=project%3Ax")

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
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, plan.Observation{PRs: map[string]plan.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(web.NewServer(store, realClock{}, nil, ""))
	t.Cleanup(srv.Close)

	candidates := fetchCandidates(t, srv,
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
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, plan.Observation{PRs: map[string]plan.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(web.NewServer(store, realClock{}, nil, ""))
	t.Cleanup(srv.Close)

	candidates := fetchCandidates(t, srv, "ticket=sandbox://CC-1&ticket=sandbox://CC-2&ticket=sandbox://CC-3")

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
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, plan.Observation{PRs: map[string]plan.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(web.NewServer(store, realClock{}, nil, ""))
	t.Cleanup(srv.Close)

	candidates := fetchCandidates(t, srv, "feature=widgets")

	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2: %+v", len(candidates), candidates)
	}
	candidateFor(t, candidates, "sandbox://CC-1")
	candidateFor(t, candidates, "sandbox://CC-2")
}
