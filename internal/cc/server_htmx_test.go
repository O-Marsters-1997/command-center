package cc_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
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

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
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

// The no-JS path is the reason handleVerb keeps its redirect: a plain form post still navigates.
func TestVerbStillRedirectsWithoutHtmx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	resp := postVerb(t, srv, "/verb?verb=kill&ticket=sandbox://CC-1", nil)
	defer func() { _ = resp.Body.Close() }()
	assertSeeOtherHome(t, resp)
}

func TestVerbSwapKeepsTheSelectedRowExpanded(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(cc.NewServer(seededStore(t, time.Now()), time.Now, nil, ""))
	t.Cleanup(srv.Close)

	q := url.Values{"verb": {"kill"}, "ticket": {"sandbox://CC-1"}, "sel": {"sandbox://CC-1"}}
	resp := postVerb(t, srv, "/verb?"+q.Encode(), map[string]string{"HX-Request": "true"})
	defer func() { _ = resp.Body.Close() }()

	if body := readBody(t, resp); !strings.Contains(body, `hx-preserve="true"`) {
		t.Errorf("the swapped board dropped the expanded detail row:\n%s", body)
	}
}

func TestBoardSwapCarriesTheBandAndMasthead(t *testing.T) {
	t.Parallel()

	server := cc.NewServer(seededStore(t, time.Now()), time.Now, nil, "")
	body := renderPath(t, server, "/board")

	for _, want := range []string{`id="masthead" hx-swap-oob="true"`, `id="band" hx-swap-oob="true"`} {
		if !strings.Contains(body, want) {
			t.Errorf("board swap does not contain %q:\n%s", want, body)
		}
	}
}

// One masthead and one band on a full page render: the OOB copies are the same templates, so a
// duplicated id would silently break every later swap.
func TestPageRendersTheMastheadAndBandExactlyOnce(t *testing.T) {
	t.Parallel()

	server := cc.NewServer(seededStore(t, time.Now()), time.Now, nil, "")
	body := renderPath(t, server, "/")

	for _, id := range []string{`id="masthead"`, `id="band"`} {
		if got := strings.Count(body, id); got != 1 {
			t.Errorf("page contains %s %d times, want 1", id, got)
		}
	}
}

func TestAgesCarryTheAbsoluteInstantForTheClock(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := seededStore(t, observedAt)
	server := cc.NewServer(store, fixedClock(observedAt.Add(90*time.Second)), nil, "")
	body := renderPath(t, server, "/")

	// The fallback text and the machine-readable instant the ticker re-reads after every swap.
	for _, want := range []string{
		`<time datetime="2026-08-20T12:00:00Z">1m30s ago</time>`,
		`<time datetime="2026-08-20T12:00:15Z">`,
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
