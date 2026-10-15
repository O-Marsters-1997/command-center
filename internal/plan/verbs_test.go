package plan_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestVerbsFitRow(t *testing.T) {
	t.Parallel()
	for _, state := range plan.AllStates() {
		t.Run(state.String(), func(t *testing.T) {
			t.Parallel()
			if group, _ := plan.RailGroup(state.Glyph()); group == "" {
				t.Errorf("RailGroup(Glyph(%s)) = empty, want a board group", state)
			}
			rowButtons := 0
			for _, v := range plan.Verbs(state) {
				if v != plan.VerbLaunch && !v.IsRowVerb() {
					t.Errorf("Verbs(%s) includes non-row verb %q", state, v)
				}
				if v != plan.VerbFollowUp {
					rowButtons++
				}
			}
			if rowButtons > 3 {
				t.Errorf("Verbs(%s) renders %d row buttons, want at most 3", state, rowButtons)
			}
		})
	}
}
