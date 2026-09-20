package cc_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
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
	tickets := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", BlockedBy: []string{"sandbox://CC-1"}},
		{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third", BlockedBy: []string{"sandbox://CC-4"}},
		{URL: "sandbox://CC-4", Repo: "cc-sandbox", Branch: "cc-4-fourth"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
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
	tickets := []cc.Ticket{
		{URL: "sandbox://PARENT", Repo: "repo", Branch: "parent"},
		{URL: "sandbox://CHILD", Repo: "repo", Branch: "child", BlockedBy: []string{"sandbox://PARENT"}},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	dispositionAsPushed(t, store, "sandbox://PARENT", at)
	const parentTip = "parent-tip"
	if err := store.RecordPush(ctx, "sandbox://PARENT", parentTip, "main", "main-tip", at); err != nil {
		t.Fatal(err)
	}

	obs := cc.Observation{
		BranchTips: map[string]string{cc.BranchKey("repo", "parent"): parentTip, cc.MainTipKey("repo"): "main-tip"},
		PRs: map[string]gh.PR{
			cc.BranchKey("repo", "parent"): {
				Number: 1, State: gh.Open, HeadOid: parentTip,
				Checks: map[string]gh.CheckState{"CI": {Status: "COMPLETED", Conclusion: "FAILURE"}},
			},
		},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	repos := []cc.Repo{{Name: "repo", Stacking: true, Checks: verdict.Predicate{Success: "CI"}}}
	srv := httptest.NewServer(cc.NewServer(store, fixedClock(at), repos, ""))
	t.Cleanup(srv.Close)

	candidates := fetchCandidates(t, srv, "ticket=sandbox://CHILD")

	child := candidateFor(t, candidates, "sandbox://CHILD")
	if child.Label != "now" || child.Base != "origin/parent" || child.BaseVerdict != "ci_failed" {
		t.Errorf("CHILD = %+v, want label now, base origin/parent, base verdict ci_failed", child)
	}
}

func TestCandidatesByFeatureReturnsEveryStoredTicketInIt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	tickets := []cc.Ticket{
		{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first", Feature: "widgets"},
		{URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2-second", Feature: "widgets"},
		{URL: "sandbox://CC-3", Repo: "cc-sandbox", Branch: "cc-3-third", Feature: "gadgets"},
	}
	if err := store.UpsertTickets(ctx, tickets); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObservation(ctx, cc.Observation{PRs: map[string]gh.PR{}}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(cc.NewServer(store, time.Now, nil, ""))
	t.Cleanup(srv.Close)

	candidates := fetchCandidates(t, srv, "feature=widgets")

	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2: %+v", len(candidates), candidates)
	}
	candidateFor(t, candidates, "sandbox://CC-1")
	candidateFor(t, candidates, "sandbox://CC-2")
}
