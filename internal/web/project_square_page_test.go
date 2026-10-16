package web_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/go-cmp/cmp"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/htmltest"
)

const (
	acmeTicket  = "https://github.com/acme/alpha/issues/1"
	looseTicket = "https://github.com/acme/solo/issues/2"
)

func squarePages(t *testing.T) (session, board string) {
	t.Helper()
	st := track(t, openStore(t), alphaRepo, soloRepo)
	storetest.Tickets(t, st,
		storepkg.Ticket{URL: acmeTicket, Repo: "acme/alpha", Branch: "one", Feature: "Billing"},
		storepkg.Ticket{URL: looseTicket, Repo: "acme/solo", Branch: "two", Feature: "Billing"},
	)
	server := openServer(st, fixedClock(testNow), "")
	form := url.Values{"name": {"acme"}, "hue": {"1"}, "repo": {"acme/alpha"}}
	if rec := do(t, server, http.MethodPost, "/projects", req{Form: form}); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /projects status = %d, want 303", rec.Code)
	}
	return renderPath(t, server, "/s/acme/alpha/1"), renderPath(t, server, "/tickets")
}

func testBoardSquares(t *testing.T) {
	t.Parallel()
	session, board := squarePages(t)

	t.Run("only the project's ticket carries a square", func(t *testing.T) {
		t.Parallel()
		var squares []string
		for _, row := range htmltest.Parse(t, board).One(t, "table#board").Find("tr.group") {
			for _, avatar := range row.Find(`[data-hue][title]`) {
				squares = append(squares, avatar.Text())
			}
		}
		if diff := cmp.Diff([]string{"A"}, squares); diff != "" {
			t.Errorf("GET /tickets squares mismatch (-want +got):\n%s", diff)
		}
	})

	for _, page := range []struct{ name, body string }{{name: "session", body: session}, {name: "board", body: board}} {
		t.Run("the "+page.name+" carries the project's square", func(t *testing.T) {
			t.Parallel()
			avatar := htmltest.Parse(t, page.body).One(t, `[data-hue="1"][title="acme"][aria-hidden="true"]`)
			if got := avatar.Text(); got != "A" {
				t.Errorf("%s avatar = %q, want A", page.name, got)
			}
		})
	}
}
