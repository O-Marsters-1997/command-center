package web_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

func testBoardGlyphs(t *testing.T) {
	t.Parallel()

	t.Run("a running row's glyph pulses", func(t *testing.T) {
		t.Parallel()
		ticket := store.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
		now := testNow.Add(90 * time.Second)
		page := renderPage(t, newServer(runningRowStore(t, ticket, testNow, now), now))

		row := htmltest.Parse(t, page).One(t, `a[data-ref]`)
		if glyphs := row.Find(`.glyph-pulse[data-glyph="running"]`); len(glyphs) != 1 {
			t.Errorf("GET /tickets running row has %d pulsing glyphs, want 1", len(glyphs))
		}
	})

	t.Run("an ended board's glyphs do not pulse", func(t *testing.T) {
		t.Parallel()
		board := htmltest.Parse(t, renderPage(t, newServer(seededStore(t, testNow), testNow))).One(t, `div#board`)

		for _, word := range []string{"ready", "blocked"} {
			board.One(t, `[data-glyph="`+word+`"]`)
		}
		if glyphs := board.Find(`.glyph-pulse`); len(glyphs) != 0 {
			t.Errorf("GET /tickets ended board has %d pulsing glyphs, want 0", len(glyphs))
		}
	})

	t.Run("every glyph word renders as a class", func(t *testing.T) {
		t.Parallel()
		body, err := web.RenderStatesBoard(plan.AllStates())
		if err != nil {
			t.Fatalf("RenderStatesBoard: %v", err)
		}

		board := htmltest.Parse(t, body).One(t, "div#board")
		for _, word := range []string{"failed", "attention", "running", "pending", "checking", "ready", "blocked", "done"} {
			if got := board.Find(`[data-glyph="` + word + `"]`); len(got) == 0 {
				t.Errorf("RenderStatesBoard(all states) has no data-glyph %s", word)
			}
		}
	})
}
