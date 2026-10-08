package web_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func TestBoardAsksBeforeOnlyDestructiveVerbs(t *testing.T) {
	t.Parallel()

	body, err := web.RenderStatesBoard([]plan.State{plan.NeedsYou, plan.PRMerged})
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.Count(body, "hx-confirm="); got != 2 {
		t.Errorf("hx-confirm attributes = %d, want 2: only kill and remove-worktree are destructive:\n%s", got, body)
	}
	if strings.Contains(body, `action="/confirm"`) {
		t.Errorf("board still links to /confirm:\n%s", body)
	}
	for _, want := range []string{"kill on ", "remove-worktree on ", "It cannot be undone."} {
		if !strings.Contains(body, want) {
			t.Errorf("board does not contain %q:\n%s", want, body)
		}
	}
}

func TestConfirmPageIsGone(t *testing.T) {
	t.Parallel()

	server := web.NewServer(seededStore(t, time.Now()), realClock{}, "")
	rec := get(t, server, "/confirm?verb=kill&ticket=sandbox://CC-1")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
