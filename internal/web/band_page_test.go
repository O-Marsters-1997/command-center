package web_test

import (
	"strings"
	"testing"
)

func TestBandRendersWrittenEmptyStatesWithNoTickets(t *testing.T) {
	t.Parallel()

	store := openStore(t)
	now := testNow
	server := newServer(store, now)

	page := renderPage(t, server)
	for _, want := range []string{
		"no tickets tracked yet",
		"no worktree has been cut",
		"no branch has reported a check yet",
		"no runs have completed this session yet",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %q:\n%s", want, page)
		}
	}
	for _, headlineWord := range []string{"yours", "deep", "green", "$"} {
		if strings.Contains(page, headlineWord) {
			t.Errorf("an empty fleet still rendered a headline instead of every card's empty copy:\n%s", page)
		}
	}
}

func TestBandRendersLiveNumbersOnceCutWorktreesAndChecksExist(t *testing.T) {
	t.Parallel()

	observedAt := testNow
	store := seededStore(t, observedAt)
	server := newServer(store, observedAt)

	page := renderPage(t, server)
	if !strings.Contains(page, "2/2 yours") {
		t.Errorf("fleet headline missing or wrong:\n%s", page)
	}
	if strings.Contains(page, "no worktree has been cut") {
		t.Error("stack card should read live numbers, since CC-1 has a cut worktree")
	}
	if !strings.Contains(page, "0 deep") {
		t.Errorf("stack headline missing or wrong:\n%s", page)
	}
}
