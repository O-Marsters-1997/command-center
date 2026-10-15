package web_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func TestRunningRowGlyphPulses(t *testing.T) {
	t.Parallel()

	ticket := store.Ticket{URL: "sandbox://CC-1", Repo: "cc-sandbox", Branch: "cc-1-first"}
	startedAt := testNow
	now := startedAt.Add(90 * time.Second)
	store := runningRowStore(t, ticket, startedAt, now)

	server := newServer(store, now)
	rec := do(t, server, http.MethodGet, "/tickets", req{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	row := htmltest.Parse(t, rec.Body).One(t, `tr.group`)
	if glyphs := row.Find(`.glyph-running.glyph-pulse`); len(glyphs) != 1 {
		t.Errorf("running row has %d pulsing glyphs, want one: %s", len(glyphs), row.HTML())
	}
}

func TestEndedRunsGlyphDoesNotPulse(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	rec := do(t, server, http.MethodGet, "/tickets", req{})
	board := htmltest.Parse(t, rec.Body).One(t, `table#board`)
	for _, word := range []string{"ready", "blocked"} {
		board.One(t, `.glyph-`+word)
	}
	if glyphs := board.Find(`.glyph-pulse`); len(glyphs) != 0 {
		t.Errorf("ended board has %d pulsing glyphs: %s", len(glyphs), glyphs[0].HTML())
	}
}

func TestBoardRendersEveryGlyphWordAsAClass(t *testing.T) {
	t.Parallel()

	body, err := web.RenderStatesBoard(plan.AllStates())
	if err != nil {
		t.Fatal(err)
	}

	board := htmltest.Parse(t, body).One(t, "table#board")
	for _, word := range []string{
		"failed", "attention", "running", "pending", "checking", "ready", "blocked", "done",
	} {
		if got := board.Find(`.glyph-` + word); len(got) == 0 {
			t.Errorf("board has no glyph for %q", word)
		}
	}
}
