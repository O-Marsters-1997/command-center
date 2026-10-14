package web_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/store/storetest"
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

	for _, tc := range []struct{ name, ticketURL, tagPattern, tag, want string }{
		{name: "titled ticket cell", ticketURL: tickets[0].URL, tagPattern: `a href="[^"]*"[^>]*`, tag: "a", want: "#100"},
		{name: "titled task cell", ticketURL: tickets[0].URL, tagPattern: "div", tag: "div",
			want: "Put each ticket&#39;s issue title on its row"},
		{name: "untitled ticket cell", ticketURL: tickets[1].URL, tagPattern: `a href="[^"]*"[^>]*`, tag: "a", want: "#101"},
		{name: "untitled task cell", ticketURL: tickets[1].URL, tagPattern: "div", tag: "div", want: "untitled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := rowCellText(t, page, tc.ticketURL, tc.tagPattern, tc.tag)
			if got != tc.want {
				t.Errorf("rowCellText = %q, want %q", got, tc.want)
			}
		})
	}
}
