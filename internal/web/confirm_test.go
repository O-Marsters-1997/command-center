package web_test

import (
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func testBoardConfirm(t *testing.T) {
	t.Parallel()

	body, err := web.RenderStatesBoard([]plan.State{plan.NeedsYou, plan.PRMerged})
	if err != nil {
		t.Fatalf("RenderStatesBoard: %v", err)
	}
	board := htmltest.Parse(t, body).One(t, "table#board")

	for _, verb := range []string{"kill", "remove-worktree"} {
		t.Run(verb+" asks first", func(t *testing.T) {
			t.Parallel()
			var confirm string
			for _, form := range board.Find(`form[hx-confirm]`) {
				if len(form.Find(`button[value="`+verb+`"]`)) == 1 {
					confirm, _ = form.Attr("hx-confirm")
				}
			}
			if !strings.Contains(confirm, verb+" on ") {
				t.Errorf("RenderStatesBoard %s hx-confirm = %q, want it to name %s on the ticket", verb, confirm, verb)
			}
		})
	}

	t.Run("no other verb asks", func(t *testing.T) {
		t.Parallel()
		if got := board.Find("[hx-confirm]"); len(got) != 2 {
			t.Errorf("RenderStatesBoard hx-confirm elements = %d, want 2", len(got))
		}
		if got := board.Find(`form[action="/confirm"]`); len(got) != 0 {
			t.Errorf("RenderStatesBoard has %d forms posting to /confirm, want 0", len(got))
		}
	})
}
