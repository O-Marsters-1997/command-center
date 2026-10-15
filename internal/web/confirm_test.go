package web_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cctest"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func TestBoardAsksBeforeOnlyDestructiveVerbs(t *testing.T) {
	t.Parallel()

	body, err := web.RenderStatesBoard([]plan.State{plan.NeedsYou, plan.PRMerged})
	if err != nil {
		t.Fatalf("RenderStatesBoard: %v", err)
	}

	board := htmltest.Parse(t, body).One(t, "table#board")
	for _, verb := range []string{"kill", "remove-worktree"} {
		button := board.One(t, `button[value="`+verb+`"]`)
		forms := board.Find(`form[hx-confirm]`)
		var confirm string
		var ok bool
		for _, form := range forms {
			if len(form.Find(`button[value="`+verb+`"]`)) == 1 {
				confirm, ok = form.Attr("hx-confirm")
			}
		}
		if !ok || !strings.Contains(confirm, verb+" on ") || !strings.Contains(confirm, "It cannot be undone.") {
			t.Errorf("%s hx-confirm = %q, present = %t: %s", verb, confirm, ok, button.HTML())
		}
	}
	if got := board.Find("[hx-confirm]"); len(got) != 2 {
		t.Errorf("hx-confirm elements = %d, want 2", len(got))
	}
	if got := board.Find(`form[action="/confirm"]`); len(got) != 0 {
		t.Errorf("obsolete confirm forms = %d: %s", len(got), got[0].HTML())
	}
}

func TestConfirmPageIsGone(t *testing.T) {
	t.Parallel()

	server := openServer(seededStore(t, testNow), cctest.Clock{At: testNow}, "")
	rec := do(t, server, http.MethodGet, "/confirm?verb=kill&ticket=sandbox://CC-1", req{})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
