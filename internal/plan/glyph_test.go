package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

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
