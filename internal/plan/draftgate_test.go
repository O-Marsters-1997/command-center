package plan_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestDraftGate(t *testing.T) {
	t.Parallel()

	blocker := plan.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	gating := []plan.Ticket{blocker}

	tests := []struct {
		name         string
		prs          map[string]plan.PRState
		verdictGreen bool
		wantDraft    bool
	}{
		{
			name:         "blocker unmerged, verdict red",
			prs:          map[string]plan.PRState{"pla-40": plan.Open},
			verdictGreen: false,
			wantDraft:    true,
		},
		{
			name:         "blocker unmerged, verdict green",
			prs:          map[string]plan.PRState{"pla-40": plan.Open},
			verdictGreen: true,
			wantDraft:    true,
		},
		{
			name:         "blocker merged, verdict red",
			prs:          map[string]plan.PRState{"pla-40": plan.Merged},
			verdictGreen: false,
			wantDraft:    true,
		},
		{
			name:         "blocker merged, verdict green",
			prs:          map[string]plan.PRState{"pla-40": plan.Merged},
			verdictGreen: true,
			wantDraft:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			draft, reason := plan.DraftGate(gating, tt.prs, tt.verdictGreen)
			if draft != tt.wantDraft {
				t.Errorf("draft = %v, want %v (reason %q)", draft, tt.wantDraft, reason)
			}
			if reason == "" {
				t.Error("reason is empty; the page renders it on a drafted row")
			}
		})
	}
}

func TestDraftGateUnresolvedBlockerNamesIt(t *testing.T) {
	t.Parallel()

	blocker := plan.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	draft, reason := plan.DraftGate([]plan.Ticket{blocker}, map[string]plan.PRState{}, true)
	if !draft {
		t.Fatal("an absent gating blocker must stay draft")
	}
	if !strings.Contains(string(reason), "sandbox://PLA-40") {
		t.Errorf("reason %q does not name the gating blocker", reason)
	}
}

func TestDraftGateVerdictNotGreenNamesOwnChecks(t *testing.T) {
	t.Parallel()

	blocker := plan.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	draft, reason := plan.DraftGate([]plan.Ticket{blocker}, map[string]plan.PRState{"pla-40": plan.Merged}, false)
	if !draft {
		t.Fatal("a red verdict must stay draft even once every gating blocker has merged")
	}
	if reason != "waiting on its own checks" {
		t.Errorf("reason = %q, want %q", reason, "waiting on its own checks")
	}
}

func TestDraftGateClosedBlockerNeverReadies(t *testing.T) {
	t.Parallel()

	blocker := plan.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	draft, reason := plan.DraftGate([]plan.Ticket{blocker}, map[string]plan.PRState{"pla-40": plan.Closed}, true)
	if !draft {
		t.Fatal("a gating blocker closed without merging must never un-draft the consumer")
	}
	if !strings.Contains(string(reason), "sandbox://PLA-40") || !strings.Contains(string(reason), "closed") {
		t.Errorf("reason %q does not name the closure", reason)
	}
}

func TestDraftGateNoGatingBlockers(t *testing.T) {
	t.Parallel()

	draft, _ := plan.DraftGate(nil, map[string]plan.PRState{}, true)
	if draft {
		t.Error("no gating blockers and a green verdict must not stay draft")
	}
	draft, reason := plan.DraftGate(nil, map[string]plan.PRState{}, false)
	if !draft {
		t.Error("no gating blockers but a red verdict must stay draft")
	}
	if reason != "waiting on its own checks" {
		t.Errorf("reason = %q, want %q", reason, "waiting on its own checks")
	}
}

func TestGatingBlockers(t *testing.T) {
	t.Parallel()

	sameRepo := plan.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	crossRepo := plan.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	consumer := plan.Ticket{
		URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2",
		BlockedBy: []string{"sandbox://CC-1", "sandbox://PLA-40"},
	}
	byURL := map[string]plan.Ticket{
		sameRepo.URL:  sameRepo,
		crossRepo.URL: crossRepo,
		consumer.URL:  consumer,
	}

	got := plan.GatingBlockers(consumer, byURL)
	if len(got) != 1 || got[0].URL != crossRepo.URL {
		t.Errorf("GatingBlockers = %+v, want only %s", got, crossRepo.URL)
	}
}

func TestOpensAsDraft(t *testing.T) {
	t.Parallel()

	blocker := plan.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	byURL := map[string]plan.Ticket{blocker.URL: blocker}

	tests := []struct {
		name   string
		ticket plan.Ticket
		want   bool
	}{
		{
			name:   "a gating edge alone opens as a draft",
			ticket: plan.Ticket{URL: "sandbox://CC-1", Repo: "repo", BlockedBy: []string{"sandbox://PLA-40"}},
			want:   true,
		},
		{
			name:   "no gating edge opens as a plain PR",
			ticket: plan.Ticket{URL: "sandbox://CC-1", Repo: "repo"},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := plan.OpensAsDraft(tt.ticket, byURL); got != tt.want {
				t.Errorf("OpensAsDraft = %v, want %v", got, tt.want)
			}
		})
	}
}
