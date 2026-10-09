package web_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

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
	rec := get(t, server, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `class="glyph glyph-running glyph-pulse"`) {
		t.Errorf("running row's glyph is not a pulsing running glyph:\n%s", body)
	}
}

func TestEndedRunsGlyphDoesNotPulse(t *testing.T) {
	t.Parallel()

	now := testNow
	server := newServer(seededStore(t, now), now)

	rec := get(t, server, "/")
	body := rec.Body.String()

	if strings.Contains(body, "glyph-pulse") {
		t.Errorf("no run is alive, but a glyph still pulses:\n%s", body)
	}
	if !strings.Contains(body, `class="glyph glyph-ready"`) {
		t.Errorf("ready row is not a ready glyph:\n%s", body)
	}
	if !strings.Contains(body, `class="glyph glyph-blocked"`) {
		t.Errorf("blocked row is not a blocked glyph:\n%s", body)
	}
}

func TestBoardRendersEveryGlyphWordAsAClass(t *testing.T) {
	t.Parallel()

	var states []plan.State
	for s := range plan.StateCount {
		states = append(states, plan.State(s))
	}
	body, err := web.RenderStatesBoard(states)
	if err != nil {
		t.Fatal(err)
	}

	for _, word := range []string{
		"failed", "attention", "running", "pending", "checking", "ready", "blocked", "done",
	} {
		if want := `class="glyph glyph-` + word + `"`; !strings.Contains(body, want) {
			t.Errorf("no row renders %s:\n%s", want, body)
		}
	}
}
