package cc

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// TestConflictingPeerHold covers the launch gate's peer decision: of two open main-based
// branches that conflict, only the lower ref proceeds, and the higher ref names it.
func TestConflictingPeerHold(t *testing.T) {
	t.Parallel()

	lower := Ticket{URL: "sandbox://CC-1", Repo: "r", Branch: "cc-1-lower"}
	higher := Ticket{URL: "sandbox://CC-2", Repo: "r", Branch: "cc-2-higher"}
	tickets := []Ticket{higher, lower} // deliberately out of ref order
	byURL := map[string]plan.Ticket{
		lower.URL:  {URL: lower.URL, Repo: lower.Repo, Branch: lower.Branch},
		higher.URL: {URL: higher.URL, Repo: higher.Repo, Branch: higher.Branch},
	}
	prs := map[string]plan.PRState{lower.Branch: plan.Open, higher.Branch: plan.Open}
	stacking := map[string]bool{"r": false}

	t.Run("a conflicting pair holds the higher ref behind the lower", func(t *testing.T) {
		t.Parallel()
		obs := Observation{ConflictsWithPeer: map[string]map[string]bool{
			branchKey("r", lower.Branch):  {branchKey("r", higher.Branch): true},
			branchKey("r", higher.Branch): {branchKey("r", lower.Branch): true},
		}}
		held := conflictingPeerHold(tickets, byURL, prs, stacking, obs)
		if held[higher.URL] != lower.Branch {
			t.Errorf("held[higher] = %q, want %q", held[higher.URL], lower.Branch)
		}
		if _, ok := held[lower.URL]; ok {
			t.Errorf("held[lower] = %q, want unheld", held[lower.URL])
		}
	})

	t.Run("a clean pair holds neither", func(t *testing.T) {
		t.Parallel()
		obs := Observation{ConflictsWithPeer: map[string]map[string]bool{
			branchKey("r", lower.Branch):  {branchKey("r", higher.Branch): false},
			branchKey("r", higher.Branch): {branchKey("r", lower.Branch): false},
		}}
		held := conflictingPeerHold(tickets, byURL, prs, stacking, obs)
		if len(held) != 0 {
			t.Errorf("held = %v, want empty", held)
		}
	})

	t.Run("a stacked branch is never a candidate", func(t *testing.T) {
		t.Parallel()
		stackedByURL := map[string]plan.Ticket{
			lower.URL: {URL: lower.URL, Repo: lower.Repo, Branch: lower.Branch},
			higher.URL: {
				URL: higher.URL, Repo: higher.Repo, Branch: higher.Branch,
				BlockedBy: []string{lower.URL},
			},
		}
		stackingOn := map[string]bool{"r": true}
		obs := Observation{ConflictsWithPeer: map[string]map[string]bool{
			branchKey("r", lower.Branch):  {branchKey("r", higher.Branch): true},
			branchKey("r", higher.Branch): {branchKey("r", lower.Branch): true},
		}}
		held := conflictingPeerHold(tickets, stackedByURL, prs, stackingOn, obs)
		if len(held) != 0 {
			t.Errorf("held = %v, want empty: higher's prospective base is lower, not main", held)
		}
	})

	t.Run("a branch with no open pull request is never a candidate", func(t *testing.T) {
		t.Parallel()
		noPR := map[string]plan.PRState{lower.Branch: plan.Open}
		obs := Observation{ConflictsWithPeer: map[string]map[string]bool{
			branchKey("r", lower.Branch):  {branchKey("r", higher.Branch): true},
			branchKey("r", higher.Branch): {branchKey("r", lower.Branch): true},
		}}
		held := conflictingPeerHold(tickets, byURL, noPR, stacking, obs)
		if len(held) != 0 {
			t.Errorf("held = %v, want empty: higher has no open pull request", held)
		}
	})

	t.Run("a chain does not serialise a ticket past the peer that holds it", func(t *testing.T) {
		t.Parallel()
		a := Ticket{URL: "sandbox://CC-1", Repo: "r", Branch: "cc-1-a"}
		b := Ticket{URL: "sandbox://CC-2", Repo: "r", Branch: "cc-2-b"}
		c := Ticket{URL: "sandbox://CC-3", Repo: "r", Branch: "cc-3-c"}
		byURL := map[string]plan.Ticket{
			a.URL: {URL: a.URL, Repo: a.Repo, Branch: a.Branch},
			b.URL: {URL: b.URL, Repo: b.Repo, Branch: b.Branch},
			c.URL: {URL: c.URL, Repo: c.Repo, Branch: c.Branch},
		}
		prs := map[string]plan.PRState{a.Branch: plan.Open, b.Branch: plan.Open, c.Branch: plan.Open}
		stacking := map[string]bool{"r": false}
		obs := Observation{ConflictsWithPeer: map[string]map[string]bool{
			branchKey("r", a.Branch): {branchKey("r", b.Branch): true, branchKey("r", c.Branch): false},
			branchKey("r", b.Branch): {branchKey("r", a.Branch): true, branchKey("r", c.Branch): true},
			branchKey("r", c.Branch): {branchKey("r", a.Branch): false, branchKey("r", b.Branch): true},
		}}
		held := conflictingPeerHold([]Ticket{c, b, a}, byURL, prs, stacking, obs)
		if held[b.URL] != a.Branch {
			t.Errorf("held[b] = %q, want %q", held[b.URL], a.Branch)
		}
		if _, ok := held[a.URL]; ok {
			t.Errorf("held[a] = %q, want unheld", held[a.URL])
		}
		if _, ok := held[c.URL]; ok {
			t.Errorf("held[c] = %q, want unheld: its only conflicting peer is itself held", held[c.URL])
		}
	})

	t.Run("ref order is by ticket number, not branch string, across a digit-count boundary", func(t *testing.T) {
		t.Parallel()
		nine := Ticket{URL: "sandbox://CC-9", Repo: "r", Branch: "cc-9-nine"}
		hundred := Ticket{URL: "sandbox://CC-100", Repo: "r", Branch: "cc-100-hundred"}
		byURL := map[string]plan.Ticket{
			nine.URL:    {URL: nine.URL, Repo: nine.Repo, Branch: nine.Branch},
			hundred.URL: {URL: hundred.URL, Repo: hundred.Repo, Branch: hundred.Branch},
		}
		prs := map[string]plan.PRState{nine.Branch: plan.Open, hundred.Branch: plan.Open}
		obs := Observation{ConflictsWithPeer: map[string]map[string]bool{
			branchKey("r", nine.Branch):    {branchKey("r", hundred.Branch): true},
			branchKey("r", hundred.Branch): {branchKey("r", nine.Branch): true},
		}}
		held := conflictingPeerHold([]Ticket{hundred, nine}, byURL, prs, stacking, obs)
		if held[hundred.URL] != nine.Branch {
			t.Errorf("held[hundred] = %q, want %q: cc-9 is the lower ref despite sorting later as a string",
				held[hundred.URL], nine.Branch)
		}
		if _, ok := held[nine.URL]; ok {
			t.Errorf("held[nine] = %q, want unheld", held[nine.URL])
		}
	})
}
