package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestState_Glyph(t *testing.T) {
	t.Parallel()

	want := map[plan.State]plan.GlyphWord{
		plan.Failed: plan.GlyphFailed, plan.PushFailed: plan.GlyphFailed,
		plan.CIFailed: plan.GlyphFailed, plan.ConflictsWithMain: plan.GlyphFailed,
		plan.RefreshConflicted:  plan.GlyphFailed,
		plan.VerificationFailed: plan.GlyphFailed, plan.Withdrawn: plan.GlyphFailed,
		plan.ReviewMe: plan.GlyphAttention, plan.NeedsYou: plan.GlyphAttention,
		plan.AwaitingInput: plan.GlyphAsked, plan.PushRefused: plan.GlyphAttention,
		plan.Parked:     plan.GlyphAttention,
		plan.AwaitingPR: plan.GlyphAttention,
		plan.Running:    plan.GlyphRunning,
		plan.Verifying:  plan.GlyphRunning, plan.WaitingToVerify: plan.GlyphPending,
		plan.PushPending: plan.GlyphPending, plan.Queued: plan.GlyphPending, plan.BaseMoved: plan.GlyphPending,
		plan.Checking: plan.GlyphChecking,
		plan.Ready:    plan.GlyphReady, plan.Cancelled: plan.GlyphReady,
		plan.Blocked: plan.GlyphBlocked, plan.WaitingOnProducerDeploy: plan.GlyphBlocked,
		plan.PRMerged: plan.GlyphDone,
	}
	if len(want) != len(plan.AllStates()) {
		t.Fatalf("table covers %d states, AllStates() has %d", len(want), len(plan.AllStates()))
	}
	for state, glyph := range want {
		t.Run(state.String(), func(t *testing.T) {
			t.Parallel()
			if got := state.Glyph(); got != glyph {
				t.Errorf("%s.Glyph() = %q, want %q", state, got, glyph)
			}
		})
	}
}

func TestOpenGlyph(t *testing.T) {
	t.Parallel()

	tests := map[plan.GlyphWord]bool{
		plan.GlyphFailed: true, plan.GlyphAttention: true, plan.GlyphAsked: true, plan.GlyphRunning: true,
		plan.GlyphPending: true, plan.GlyphChecking: true, plan.GlyphReady: true, plan.GlyphBlocked: true,
		plan.GlyphDone: false, plan.GlyphUnknown: false,
	}
	for glyph, want := range tests {
		t.Run(string(glyph), func(t *testing.T) {
			t.Parallel()
			if got := plan.OpenGlyph(glyph); got != want {
				t.Errorf("OpenGlyph(%q) = %t, want %t", glyph, got, want)
			}
		})
	}
}

func TestRailGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		glyph  plan.GlyphWord
		group  string
		folded bool
	}{
		{glyph: plan.GlyphFailed, group: "needs-you"},
		{glyph: plan.GlyphAttention, group: "needs-you"},
		{glyph: plan.GlyphAsked, group: "needs-you"},
		{glyph: plan.GlyphRunning, group: "in-flight"},
		{glyph: plan.GlyphPending, group: "in-flight"},
		{glyph: plan.GlyphChecking, group: "in-flight"},
		{glyph: plan.GlyphReady, group: "in-flight", folded: true},
		{glyph: plan.GlyphBlocked, group: "in-flight", folded: true},
		{glyph: plan.GlyphDone, group: "settled"},
	}
	for _, tt := range tests {
		t.Run(string(tt.glyph), func(t *testing.T) {
			t.Parallel()
			group, folded := plan.RailGroup(tt.glyph)
			if group != tt.group || folded != tt.folded {
				t.Errorf("RailGroup(%q) = %q, %t, want %q, %t", tt.glyph, group, folded, tt.group, tt.folded)
			}
		})
	}
}
