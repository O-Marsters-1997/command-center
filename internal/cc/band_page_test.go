package cc_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

// TestBandRendersWrittenEmptyStatesWithNoTickets covers issue #106 AC5: a project with no
// tickets at all — so no worktree has ever been cut and no check has ever reported — reads each
// card's own written copy rather than a blank frame or a bare "–".
func TestBandRendersWrittenEmptyStatesWithNoTickets(t *testing.T) {
	t.Parallel()

	store := openStore(t, filepath.Join(t.TempDir(), "cc.db"))
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	server := cc.NewServer(store, fixedClock(now), nil, "")

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
	if strings.Contains(page, `<p class="headline">`) {
		t.Errorf("an empty fleet still rendered a headline instead of every card's empty copy:\n%s", page)
	}
}

// TestBandRendersLiveNumbersOnceCutWorktreesAndChecksExist covers issue #106 AC2-AC4 against the
// board's own two-row fixture: the fleet ribbon sums to the row count, "yours" counts the row
// that is not unattended, and the stack card reports off the one row with a cut worktree.
func TestBandRendersLiveNumbersOnceCutWorktreesAndChecksExist(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := seededStore(t, observedAt)
	server := cc.NewServer(store, fixedClock(observedAt), nil, "")

	page := renderPage(t, server)
	if !strings.Contains(page, `<p class="headline">2/2 yours</p>`) {
		t.Errorf("fleet headline missing or wrong:\n%s", page)
	}
	if strings.Contains(page, "no worktree has been cut") {
		t.Error("stack card should read live numbers, since CC-1 has a cut worktree")
	}
	if !strings.Contains(page, `<p class="headline">0 deep</p>`) {
		t.Errorf("stack headline missing or wrong:\n%s", page)
	}
}
