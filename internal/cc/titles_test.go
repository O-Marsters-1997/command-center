package cc_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

// TestBoardNamesEachTicketByItsIssueTitle covers issue #100's first three acceptance criteria.
func TestBoardNamesEachTicketByItsIssueTitle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	tasks := []cc.Task{
		{TicketURL: "https://github.com/owner/repo/issues/100", Repo: "repo", Branch: "cc-100"},
		{TicketURL: "https://github.com/owner/repo/issues/101", Repo: "repo", Branch: "cc-101"},
	}
	if err := store.UpsertTasks(ctx, tasks); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	obs := cc.Observation{
		ObservedAt: at,
		Titles: map[string]string{
			"https://github.com/owner/repo/issues/100": "Put each ticket's issue title on its row",
			"https://github.com/owner/repo/issues/999": "An issue no task on the board is working on",
		},
	}
	if err := store.SaveObservation(ctx, obs); err != nil {
		t.Fatal(err)
	}

	page := boardFor(t, store)

	if got, want := rowTicket(t, page, tasks[0].TicketURL),
		"#100 Put each ticket&#39;s issue title on its row"; got != want {
		t.Errorf("titled row's ticket cell = %q, want %q", got, want)
	}
	if got, want := rowTicket(t, page, tasks[1].TicketURL), "#101"; got != want {
		t.Errorf("untitled row's ticket cell = %q, want %q (its number alone)", got, want)
	}
}
