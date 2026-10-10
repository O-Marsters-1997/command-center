package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func postVerb(t *testing.T, srv *httptest.Server, target string, headers map[string]string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, srv.URL+target, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", srv.URL)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := noRedirect(srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestVerbAnswersAnHtmxRequestWithTheBoard(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(openServer(seededRunning(t), realClock{}, ""))
	t.Cleanup(srv.Close)

	resp := postVerb(t, srv, "/verb?verb=kill&ticket=sandbox://CC-1", map[string]string{"HX-Request": "true"})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readBody(t, resp)
	if !strings.HasPrefix(strings.TrimSpace(body), `<table id="board"`) {
		t.Errorf("htmx verb response is not the board fragment:\n%s", body)
	}
	if !strings.Contains(body, "kill queued") {
		t.Errorf("the swapped board does not show the queued verb:\n%s", body)
	}
}

func TestVerbStillRedirectsWithoutHtmx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(openServer(seededRunning(t), realClock{}, ""))
	t.Cleanup(srv.Close)

	resp := postVerb(t, srv, "/verb?verb=kill&ticket=sandbox://CC-1", nil)
	defer func() { _ = resp.Body.Close() }()
	assertSeeOtherHome(t, resp)
}

func TestBoardSwapIsTheTableAlone(t *testing.T) {
	t.Parallel()

	server := openServer(seededStore(t, time.Now()), realClock{}, "")
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

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
