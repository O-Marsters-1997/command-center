package web_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func TestBoardNamesEachTicketByItsIssueTitle(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	tickets := []store.Ticket{
		{URL: "https://github.com/owner/repo/issues/100", Repo: "repo", Branch: "cc-100"},
		{URL: "https://github.com/owner/repo/issues/101", Repo: "repo", Branch: "cc-101"},
	}
	if err := st.UpsertTickets(t.Context(), tickets); err != nil {
		t.Fatal(err)
	}
	obs := plan.Observation{
		ObservedAt: testNow,
		Titles: map[string]string{
			tickets[0].URL: "Put each ticket's issue title on its row",
			"https://github.com/owner/repo/issues/999": "An issue no ticket on the board is working on",
		},
	}
	if err := st.SaveObservation(t.Context(), obs); err != nil {
		t.Fatal(err)
	}

	page := renderBoard(t, newServer(st, testNow))

	for _, tc := range []struct{ name, got, want string }{
		{"titled ticket cell", rowCellText(t, page, tickets[0].URL, `button type="button"[^>]*`, "button"), "#100"},
		{"titled task cell", rowCellText(t, page, tickets[0].URL, "div", "div"), "Put each ticket&#39;s issue title on its row"},
		{"untitled ticket cell", rowCellText(t, page, tickets[1].URL, `button type="button"[^>]*`, "button"), "#101"},
		{"untitled task cell", rowCellText(t, page, tickets[1].URL, "div", "div"), "untitled"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
