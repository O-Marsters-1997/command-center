package web_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
	"github.com/O-Marsters-1997/command-center/internal/web/webtest"
)

func TestBoardNamesEachTicketByItsIssueTitle(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	tickets := []store.Ticket{
		{URL: "https://github.com/owner/repo/issues/100", Repo: "repo", Branch: "cc-100"},
		{URL: "https://github.com/owner/repo/issues/101", Repo: "repo", Branch: "cc-101"},
	}
	storetest.Tickets(t, st, tickets...)
	obs := plan.Observation{
		ObservedAt: testNow,
		Titles: map[plan.TicketURL]string{
			plan.TicketURL(tickets[0].URL):             "Put each ticket's issue title on its row",
			"https://github.com/owner/repo/issues/999": "An issue no ticket on the board is working on",
		},
	}
	if err := st.SaveObservation(t.Context(), obs); err != nil {
		t.Fatal(err)
	}

	page := renderBoard(t, newServer(st, testNow))

	for _, tc := range []struct{ name, ticketURL, ref, title string }{
		{"titled", tickets[0].URL, "#100", "Put each ticket's issue title on its row"},
		{"untitled", tickets[1].URL, "#101", "untitled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			row := webtest.RowFor(t, page, tc.ticketURL)
			if row.Ref != tc.ref {
				t.Errorf("ref = %q, want %q:\n%s", row.Ref, tc.ref, row.Row.HTML())
			}
			divs := row.Ticket.Find("div")
			if len(divs) == 0 {
				t.Fatalf("ticket cell has no title div:\n%s", row.Row.HTML())
			}
			if got := divs[len(divs)-1].Text(); got != tc.title {
				t.Errorf("title = %q, want %q:\n%s", got, tc.title, row.Row.HTML())
			}
		})
	}
}
