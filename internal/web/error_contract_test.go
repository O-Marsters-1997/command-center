package web_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
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

	rec := do(t, server, http.MethodGet, "/tickets", req{})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET /tickets on a closed store: status = %d, want 500", rec.Code)
	}
	if got := rec.Body; got != "internal error\n" {
		t.Errorf("500 body = %q, want only \"internal error\"", got)
	}
	if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "closed") {
		t.Errorf("500 not logged with its cause: %q", logs.String())
	}
}

func TestRefusedVerbOnHTMXIsAFlashFragment(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})
	srv := newServer(st, testNow)

	rec := do(t, srv, http.MethodPost, "/verb", req{
		Form:   url.Values{"verb": {"bogus-verb"}, "ticket": {sessionTicket}},
		Header: http.Header{"Hx-Request": {"true"}},
	})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("refused verb: status = %d, want 422", rec.Code)
	}
	if got := rec.Header.Get("HX-Retarget"); got != "#flash" {
		t.Errorf("HX-Retarget = %q, want #flash", got)
	}
	if got := rec.Header.Get("HX-Reselect"); got != "p.banner" {
		t.Errorf("HX-Reselect = %q, want p.banner so a link's hx-select cannot empty the flash", got)
	}
	if got := rec.Header.Get("HX-Reswap"); got != "innerHTML" {
		t.Errorf("HX-Reswap = %q, want innerHTML", got)
	}
	banner := htmltest.Parse(t, rec.Body).One(t, "p.banner")
	if got := banner.Text(); got != `unsupported verb "bogus-verb"` {
		t.Errorf("flash refusal = %q: %s", got, banner.HTML())
	}
}

func TestRefusedVerbWithoutHTMXKeepsItsStatus(t *testing.T) {
	t.Parallel()
	st, _ := sessionStore(t, sessionRunOpts{Kind: "agent", Prompt: "p", KeepPrompt: true, Alive: true})

	rec := do(t, newServer(st, testNow), http.MethodPost, "/verb", req{
		Form: url.Values{"verb": {"bogus-verb"}, "ticket": {sessionTicket}},
	})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("refused verb without htmx: status = %d, want 400", rec.Code)
	}
}

func TestLayoutSwapsRefusalResponses(t *testing.T) {
	t.Parallel()
	body := renderPage(t, newServer(openStore(t), testNow))
	root := htmltest.Parse(t, body)
	flash := root.One(t, "#flash")
	if got, _ := flash.Attr("aria-live"); got != "polite" {
		t.Errorf("flash aria-live = %q: %s", got, flash.HTML())
	}
	meta := root.One(t, `meta[name="htmx-config"]`)
	if config, _ := meta.Attr("content"); !strings.Contains(config, `"code":"422"`) {
		t.Errorf("htmx config lacks 422 response handling: %s", meta.HTML())
	}
}
