package plan_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestDraftGate(t *testing.T) {
	t.Parallel()

	blocker := plan.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	gating := []plan.Ticket{blocker}

	tests := []struct {
		name         string
		gating       []plan.Ticket
		prs          map[string]plan.PRState
		verdictGreen bool
		wantDraft    bool
		reasonHas    []string
	}{
		{
			name: "blocker unmerged, verdict red", gating: gating,
			prs: map[string]plan.PRState{"pla-40": plan.Open}, wantDraft: true,
		},
		{
			name: "blocker unmerged, verdict green", gating: gating,
			prs: map[string]plan.PRState{"pla-40": plan.Open}, verdictGreen: true, wantDraft: true,
		},
		{
			name: "blocker merged, verdict red", gating: gating,
			prs: map[string]plan.PRState{"pla-40": plan.Merged}, wantDraft: true,
		},
		{
			name: "blocker merged, verdict green", gating: gating,
			prs: map[string]plan.PRState{"pla-40": plan.Merged}, verdictGreen: true,
		},
		{
			name: "an absent gating blocker stays draft and is named", gating: gating,
			prs: map[string]plan.PRState{}, verdictGreen: true, wantDraft: true,
			reasonHas: []string{"sandbox://PLA-40"},
		},
		{
			name: "a blocker closed without merging never readies", gating: gating,
			prs: map[string]plan.PRState{"pla-40": plan.Closed}, verdictGreen: true, wantDraft: true,
			reasonHas: []string{"sandbox://PLA-40", "closed"},
		},
		{
			name: "no gating blockers and a green verdict is ready",
			prs:  map[string]plan.PRState{}, verdictGreen: true,
		},
		{
			name: "no gating blockers and a red verdict waits on its own checks",
			prs:  map[string]plan.PRState{}, wantDraft: true,
			reasonHas: []string{"waiting on its own checks"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			draft, reason := plan.DraftGate(tt.gating, tt.prs, tt.verdictGreen)
			if draft != tt.wantDraft {
				t.Errorf("DraftGate(%v, green %t) draft = %t, want %t (reason %q)",
					tt.prs, tt.verdictGreen, draft, tt.wantDraft, reason)
			}
			if reason == "" {
				t.Errorf("DraftGate(%v, green %t) reason = \"\", want one", tt.prs, tt.verdictGreen)
			}
			for _, want := range tt.reasonHas {
				if !strings.Contains(string(reason), want) {
					t.Errorf("DraftGate(%v, green %t) reason = %q, want it to name %q", tt.prs, tt.verdictGreen, reason, want)
				}
			}
		})
	}
}

func TestGatingBlockers(t *testing.T) {
	t.Parallel()

	sameRepo := plan.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1"}
	crossRepo := plan.Ticket{URL: "sandbox://PLA-40", Repo: "services", Branch: "pla-40"}
	consumer := plan.Ticket{
		URL: "sandbox://CC-2", Repo: "cc-sandbox", Branch: "cc-2",
		BlockedBy: []plan.TicketURL{"sandbox://CC-1", "sandbox://PLA-40"},
	}
	byURL := map[plan.TicketURL]plan.Ticket{
		sameRepo.URL:  sameRepo,
		crossRepo.URL: crossRepo,
		consumer.URL:  consumer,
	}

	if diff := cmp.Diff([]plan.Ticket{crossRepo}, plan.GatingBlockers(consumer, byURL),
		cmpopts.EquateComparable(plan.AgentChoice{})); diff != "" {
		t.Errorf("GatingBlockers(%s) mismatch (-want +got):\n%s", consumer.URL, diff)
	}
}
