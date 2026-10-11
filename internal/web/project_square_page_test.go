package web_test

import (
	"net/url"
	"strings"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

const (
	acmeTicket  = "https://github.com/acme/alpha/issues/1"
	looseTicket = "https://github.com/acme/solo/issues/2"
)

func squareServer(t *testing.T) string {
	t.Helper()
	st := track(t, openStore(t), alphaRepo, soloRepo)
	if err := st.UpsertTickets(t.Context(), []storepkg.Ticket{
		{URL: acmeTicket, Repo: "acme/alpha", Branch: "one", Feature: "Billing"},
		{URL: looseTicket, Repo: "acme/solo", Branch: "two", Feature: "Billing"},
	}); err != nil {
		t.Fatal(err)
	}
	server := openServer(st, fixedClock(testNow), "")
	rec := postProjectForm(t, server, url.Values{"name": {"acme"}, "hue": {"1"}, "repo": {"acme/alpha"}})
	if rec.Code != 303 {
		t.Fatalf("POST /projects = %d, want 303: %s", rec.Code, rec.Body)
	}
	return renderPath(t, server, "/s/acme/alpha/1") + "\x00" + renderPath(t, server, "/tickets")
}

func TestSessionAndBoardCarryTheTicketsSquare(t *testing.T) {
	t.Parallel()
	pages := strings.Split(squareServer(t), "\x00")
	square := `<span class="avatar" data-hue="1" title="acme" aria-hidden="true">A</span>`

	if got := strings.Count(pages[1], `class="avatar" data-hue="1" title=`); got != 1 {
		t.Errorf("board has %d squares, want 1: the loose repo's ticket carries none", got)
	}
	for name, body := range map[string]string{"session": pages[0], "board": pages[1]} {
		if !strings.Contains(body, square) {
			t.Errorf("%s page missing %q:\n%s", name, square, body)
		}
	}
}
