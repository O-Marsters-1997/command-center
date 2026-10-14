package web_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
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
	if !strings.HasPrefix(strings.TrimSpace(body), `<table id="board"`) {
		t.Errorf("htmx verb response is not the board fragment:\n%s", body)
	}
	if !strings.Contains(body, "kill queued") {
		t.Errorf("the swapped board does not show the queued verb:\n%s", body)
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

	if !strings.HasPrefix(strings.TrimSpace(body), `<table id="board"`) {
		t.Errorf("board swap is not the table:\n%s", body)
	}
	if strings.Contains(body, "hx-swap-oob") {
		t.Errorf("board swap still carries an out-of-band region:\n%s", body)
	}
}

func TestAgesCarryTheAbsoluteInstantForTheClock(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	store := seededStore(t, observedAt)
	server := newServer(store, observedAt.Add(90*time.Second))
	body := renderPath(t, server, "/rail")

	for _, want := range []string{
		`<time datetime="2026-08-20T12:00:15Z">1m15s ago</time>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page does not contain %q:\n%s", want, body)
		}
	}
}
