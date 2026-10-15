package web_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
)

func TestVerbAnswersAnHtmxRequestWithTheBoard(t *testing.T) {
	t.Parallel()

	server := newServer(seededRunning(t), testNow)
	resp := do(t, server, http.MethodPost, "/verb?verb=kill&ticket=sandbox://CC-1", req{
		Header: http.Header{"Hx-Request": {"true"}},
	})

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	body := resp.Body
	root := htmltest.Parse(t, body)
	board := root.One(t, "table#board")
	if got := strings.TrimSpace(body); !strings.HasPrefix(got, "<table") {
		t.Errorf("htmx verb response is not a table fragment: %q", got[:min(len(got), 120)])
	}
	if got := board.Text(); !strings.Contains(got, "kill queued") {
		t.Error("board does not show queued kill")
	}
}

func TestVerbStillRedirectsWithoutHtmx(t *testing.T) {
	t.Parallel()

	server := newServer(seededRunning(t), testNow)
	resp := do(t, server, http.MethodPost, "/verb?verb=kill&ticket=sandbox://CC-1", req{})
	if resp.Code != http.StatusSeeOther || resp.Location != "/" {
		t.Errorf("POST /verb = (%d, %q), want (303, /)", resp.Code, resp.Location)
	}
}

func TestBoardSwapIsTheTableAlone(t *testing.T) {
	t.Parallel()

	server := newServer(seededStore(t, testNow), testNow)
	body := renderPath(t, server, "/board")

	root := htmltest.Parse(t, body)
	root.One(t, "table#board")
	if got := strings.TrimSpace(body); !strings.HasPrefix(got, "<table") {
		t.Errorf("board swap is not a table fragment: %q", got[:min(len(got), 120)])
	}
	if regions := root.Find("[hx-swap-oob]"); len(regions) != 0 {
		t.Errorf("board swap carries an out-of-band region: %s", regions[0].HTML())
	}
}

func TestAgesCarryTheAbsoluteInstantForTheClock(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	store := seededStore(t, observedAt)
	server := newServer(store, observedAt.Add(90*time.Second))
	body := renderPath(t, server, "/rail")

	clock := htmltest.Parse(t, body).One(t, `time[datetime="2026-08-20T12:00:15Z"]`)
	if got := clock.Text(); got != "1m15s ago" {
		t.Errorf("rail clock = %q: %s", got, clock.HTML())
	}
}
