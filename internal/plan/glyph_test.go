package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestGlyph(t *testing.T) {
	t.Parallel()

	want := map[plan.State]string{
		plan.Failed: "failed", plan.CutFailed: "failed", plan.PushFailed: "failed", plan.CIFailed: "failed",
		plan.ConflictsWithMain: "failed", plan.RefreshConflicted: "failed", plan.BaseGone: "failed",
		plan.VerificationFailed: "failed", plan.PRClosedUnmerged: "failed",
		plan.ReviewMe: "attention", plan.NeedsYou: "attention", plan.ConflictResolved: "attention",
		plan.Running:     "running",
		plan.PushPending: "pending", plan.Queued: "pending", plan.BaseMoved: "pending",
		plan.Checking: "checking",
		plan.Ready:    "ready", plan.Cancelled: "ready",
		plan.Blocked: "blocked", plan.WaitingOnProducerDeploy: "blocked",
		plan.PRMerged: "done",
	}
	if len(want) != plan.StateCount {
		t.Fatalf("table covers %d states, the enum has %d", len(want), plan.StateCount)
	}
	for state, glyph := range want {
		if got := plan.Glyph(state); got != glyph {
			t.Errorf("Glyph(%s) = %q, want %q", state, got, glyph)
		}
	}
}

func TestRailGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		glyph  string
		group  string
		folded bool
	}{
		{"failed", "needs-you", false},
		{"attention", "needs-you", false},
		{"running", "in-flight", false},
		{"pending", "in-flight", false},
		{"checking", "in-flight", false},
		{"ready", "in-flight", true},
		{"blocked", "in-flight", true},
		{"done", "settled", false},
	}
	for _, tt := range tests {
		group, folded := plan.RailGroup(tt.glyph)
		if group != tt.group || folded != tt.folded {
			t.Errorf("RailGroup(%q) = %q, %t; want %q, %t", tt.glyph, group, folded, tt.group, tt.folded)
		}
	}
}
