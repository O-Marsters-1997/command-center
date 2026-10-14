package web_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/web"
)

func TestServerErrorHidesCauseAndLogsIt(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	var logs bytes.Buffer
	server := web.NewServer(web.Deps{
		Logger: slog.New(slog.NewTextHandler(&logs, nil)), Store: st, Clock: fixedClock(testNow),
	})
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	rec := get(t, server, "/tickets")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET /tickets on a closed store: status = %d, want 500", rec.Code)
	}
	if got := rec.Body.String(); got != "internal error\n" {
		t.Errorf("500 body = %q, want only \"internal error\"", got)
	}
	if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "closed") {
		t.Errorf("500 not logged with its cause: %q", logs.String())
	}
}

func TestRefusedVerbOnHTMXIsAFlashFragment(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, "agent", "p", true, true)
	srv := newServer(st, testNow)

	rec := sessionVerb(t, srv, url.Values{"verb": {"bogus-verb"}, "ticket": {sessionTicket}}, true)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("refused verb: status = %d, want 422", rec.Code)
	}
	if got := rec.Header().Get("HX-Retarget"); got != "#flash" {
		t.Errorf("HX-Retarget = %q, want #flash", got)
	}
	if got := rec.Header().Get("HX-Reselect"); got != "p.banner" {
		t.Errorf("HX-Reselect = %q, want p.banner so a link's hx-select cannot empty the flash", got)
	}
	if got := rec.Header().Get("HX-Reswap"); got != "innerHTML" {
		t.Errorf("HX-Reswap = %q, want innerHTML", got)
	}
	if !strings.Contains(rec.Body.String(), `unsupported verb &#34;bogus-verb&#34;`) {
		t.Errorf("flash does not carry the refusal: %q", rec.Body)
	}
}

func TestRefusedVerbWithoutHTMXKeepsItsStatus(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, "agent", "p", true, true)

	rec := sessionVerb(t, newServer(st, testNow), url.Values{"verb": {"bogus-verb"}, "ticket": {sessionTicket}}, false)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("refused verb without htmx: status = %d, want 400", rec.Code)
	}
}

func TestLayoutSwapsRefusalResponses(t *testing.T) {
	t.Parallel()
	body := renderPage(t, newServer(openStore(t), testNow))
	for _, want := range []string{`name="htmx-config"`, `"code":"422"`, `id="flash"`, `aria-live="polite"`} {
		if !strings.Contains(body, want) {
			t.Errorf("layout lacks %s", want)
		}
	}
}
