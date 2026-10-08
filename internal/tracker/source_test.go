package tracker

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func fakeRun(t *testing.T) func(ctx context.Context, args ...string) ([]byte, error) {
	t.Helper()
	return func(_ context.Context, args ...string) ([]byte, error) {
		switch {
		case args[0] == "label" && args[1] == "list":
			return readFixture(t, "label_list.json"), nil
		case args[0] == "issue" && args[1] == "list":
			return readFixture(t, "issue_list_ready_and_beyond.json"), nil
		case args[0] == "api" && strings.HasSuffix(args[1], "/issues/120/dependencies/blocked_by"):
			return readFixture(t, "blocked_by_empty.json"), nil
		case args[0] == "api" && strings.HasSuffix(args[1], "/issues/121/dependencies/blocked_by"):
			return readFixture(t, "blocked_by_121.json"), nil
		case args[0] == "api" && strings.HasSuffix(args[1], "/issues/122/dependencies/blocked_by"):
			return readFixture(t, "blocked_by_empty.json"), nil
		case args[0] == "api" && strings.HasSuffix(args[1], "/issues/123/dependencies/blocked_by"):
			return readFixture(t, "blocked_by_empty.json"), nil
		case args[0] == "api" && strings.HasSuffix(args[1], "/issues/105/dependencies/blocked_by"):
			return readFixture(t, "blocked_by_105.json"), nil
		default:
			return nil, fmt.Errorf("fakeRun: no fixture for gh %s", strings.Join(args, " "))
		}
	}
}

func TestGithubSourceFeatures(t *testing.T) {
	t.Parallel()

	src := &githubSource{owner: "O-Marsters-1997", repo: "command-center", run: fakeRun(t)}
	got, err := src.Features(t.Context())
	if err != nil {
		t.Fatalf("Features: %v", err)
	}

	want := []Feature{"project:repo-and-ticket-model", "project:fleet-view"}
	if !slices.Equal(got, want) {
		t.Errorf("Features() = %v, want %v", got, want)
	}
}

func TestGithubSourceTickets(t *testing.T) {
	t.Parallel()

	src := &githubSource{owner: "O-Marsters-1997", repo: "command-center", run: fakeRun(t)}
	got, err := src.Tickets(t.Context(), "project:repo-and-ticket-model")
	if err != nil {
		t.Fatalf("Tickets: %v", err)
	}

	want := []Ticket{
		{
			URL:       "https://github.com/O-Marsters-1997/command-center/issues/120",
			Number:    120,
			Title:     "Add the tracker package",
			Body:      "Owns everything about reading an issue tracker.",
			Status:    "ready",
			BlockedBy: []string{},
		},
		{
			URL:       "https://github.com/O-Marsters-1997/command-center/issues/121",
			Number:    121,
			Title:     "Wire the import route",
			Body:      "Renders the tracker groups available.",
			Status:    "in-progress",
			BlockedBy: []string{},
		},
		{
			URL:       "https://github.com/O-Marsters-1997/command-center/issues/122",
			Number:    122,
			Title:     "Sketch a future idea",
			Body:      "Not scoped yet.",
			Status:    "backlog",
			BlockedBy: []string{},
		},
		{
			URL:       "https://github.com/O-Marsters-1997/command-center/issues/123",
			Number:    123,
			Title:     "Uncategorised ticket",
			Body:      "No status label at all.",
			Status:    "",
			BlockedBy: []string{},
		},
	}
	if len(got) != len(want) {
		t.Fatalf("Tickets() returned %d tickets (%v), want %d", len(got), got, len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.URL != w.URL || g.Number != w.Number || g.Title != w.Title || g.Body != w.Body ||
			g.Status != w.Status || !slices.Equal(g.BlockedBy, w.BlockedBy) || (g.BlockedBy == nil) != (w.BlockedBy == nil) {
			t.Errorf("ticket[%d] = %+v, want %+v", i, g, w)
		}
	}
}
