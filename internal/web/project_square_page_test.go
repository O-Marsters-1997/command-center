package web_test

import (
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/htmltest"
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
		t.Fatalf("POST /projects = %d, want 303", rec.Code)
	}
	return renderPath(t, server, "/s/acme/alpha/1"), renderPath(t, server, "/tickets")
}

func TestSessionAndBoardCarryTheTicketsSquare(t *testing.T) {
	t.Parallel()
	session, board := squareServer(t)
	rows := htmltest.Parse(t, board).One(t, "table#board").Find("tr.group")
	var squares []string
	for _, row := range rows {
		for _, avatar := range row.Find(`[data-hue][title]`) {
			squares = append(squares, avatar.Text())
		}
	}
	if !slices.Equal(squares, []string{"A"}) {
		t.Errorf("board squares = %q, want [A]", squares)
	}
	for _, page := range []struct{ name, body string }{{name: "session", body: session}, {name: "board", body: board}} {
		t.Run(page.name, func(t *testing.T) {
			t.Parallel()
			avatar := htmltest.Parse(t, page.body).One(t, `[data-hue="1"][title="acme"][aria-hidden="true"]`)
			if got := avatar.Text(); got != "A" {
				t.Errorf("project avatar = %q: %s", got, avatar.HTML())
			}
		})
	}
}
