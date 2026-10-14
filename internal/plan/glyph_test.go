package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestGlyph(t *testing.T) {
	t.Parallel()

	want := map[plan.State]plan.GlyphWord{
		plan.Failed: plan.GlyphFailed, plan.CutFailed: plan.GlyphFailed, plan.PushFailed: plan.GlyphFailed,
		plan.CIFailed: plan.GlyphFailed, plan.ConflictsWithMain: plan.GlyphFailed,
		plan.RefreshConflicted: plan.GlyphFailed, plan.BaseGone: plan.GlyphFailed,
		plan.VerificationFailed: plan.GlyphFailed, plan.PRClosedUnmerged: plan.GlyphFailed,
		plan.ReviewMe: plan.GlyphAttention, plan.NeedsYou: plan.GlyphAttention,
		plan.AwaitingInput: plan.GlyphAttention,
		plan.Running:       plan.GlyphRunning,
		plan.PushPending:   plan.GlyphPending, plan.Queued: plan.GlyphPending, plan.BaseMoved: plan.GlyphPending,
		plan.Checking: plan.GlyphChecking,
		plan.Ready:    plan.GlyphReady, plan.Cancelled: plan.GlyphReady,
		plan.Blocked: plan.GlyphBlocked, plan.WaitingOnProducerDeploy: plan.GlyphBlocked,
		plan.PRMerged: plan.GlyphDone,
	}
	if len(want) != plan.StateCount {
		t.Fatalf("table covers %d states, the enum has %d", len(want), plan.StateCount)
	}
	for state, glyph := range want {
		if got := state.Glyph(); got != glyph {
			t.Errorf("Glyph(%s) = %q, want %q", state, got, glyph)
		}
	}
}

func TestRailGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		glyph  plan.GlyphWord
		group  string
		folded bool
	}{
		{plan.GlyphFailed, "needs-you", false},
		{plan.GlyphAttention, "needs-you", false},
		{plan.GlyphRunning, "in-flight", false},
		{plan.GlyphPending, "in-flight", false},
		{plan.GlyphChecking, "in-flight", false},
		{plan.GlyphReady, "in-flight", true},
		{plan.GlyphBlocked, "in-flight", true},
		{plan.GlyphDone, "settled", false},
	}
	for _, tt := range tests {
		group, folded := plan.RailGroup(tt.glyph)
		if group != tt.group || folded != tt.folded {
			t.Errorf("RailGroup(%q) = %q, %t; want %q, %t", tt.glyph, group, folded, tt.group, tt.folded)
		}
	}
}
