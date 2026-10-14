package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
)

const (
	acmeTicket  = "https://github.com/acme/alpha/issues/1"
	looseTicket = "https://github.com/acme/solo/issues/2"
)

func squareServer(t *testing.T) (session, board string) {
	t.Helper()
	st := track(t, openStore(t), alphaRepo, soloRepo)
	storetest.Tickets(t, st,
		storepkg.Ticket{URL: acmeTicket, Repo: "acme/alpha", Branch: "one", Feature: "Billing"},
		storepkg.Ticket{URL: looseTicket, Repo: "acme/solo", Branch: "two", Feature: "Billing"},
	)
	server := openServer(st, fixedClock(testNow), "")
	form := url.Values{"name": {"acme"}, "hue": {"1"}, "repo": {"acme/alpha"}}
	rec := do(t, server, http.MethodPost, "/projects", req{Form: form})
	if rec.Code != 303 {
		t.Fatalf("POST /projects = %d, want 303: %s", rec.Code, rec.Body)
	}
	return renderPath(t, server, "/s/acme/alpha/1"), renderPath(t, server, "/tickets")
}

func TestSessionAndBoardCarryTheTicketsSquare(t *testing.T) {
	t.Parallel()
	session, board := squareServer(t)
	square := `<span class="avatar" data-hue="1" title="acme" aria-hidden="true">A</span>`

	if got := strings.Count(board, `class="avatar" data-hue="1" title=`); got != 1 {
		t.Errorf("board has %d squares, want 1: the loose repo's ticket carries none", got)
	}
	for _, page := range []struct{ name, body string }{{name: "session", body: session}, {name: "board", body: board}} {
		t.Run(page.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(page.body, square) {
				t.Errorf("page missing %q:\n%s", square, page.body)
			}
		})
	}
}
