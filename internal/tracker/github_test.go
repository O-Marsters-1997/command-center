package tracker_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/command"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/tracker/trackertest"
)

const remote = "github.com/O-Marsters-1997/command-center"

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", name, err)
	}
	return raw
}

func sourceReplaying(t *testing.T, respond func(args []string) ([]byte, error)) tracker.Source {
	t.Helper()
	run := func(_ context.Context, _ time.Duration, _, name string, args ...string) ([]byte, error) {
		if name != "gh" {
			return nil, fmt.Errorf("ran %q, want gh", name)
		}
		return respond(args)
	}
	src, err := tracker.Resolve(gh.CLI{Exec: command.ExecutorFunc(run)})(tracker.GitHub, remote)
	if err != nil {
		t.Fatalf("Resolve(%s): %v", remote, err)
	}
	return src
}

func recordedGitHub(t *testing.T) func(args []string) ([]byte, error) {
	t.Helper()
	blockedBy := map[string]string{
		"120": "blocked_by_empty.json", "121": "blocked_by_121.json", "122": "blocked_by_empty.json",
		"123": "blocked_by_empty.json", "105": "blocked_by_105.json",
	}
	return func(args []string) ([]byte, error) {
		switch {
		case slices.Equal(args[:2], []string{"label", "list"}):
			return readFixture(t, "label_list.json"), nil
		case slices.Equal(args[:2], []string{"issue", "list"}):
			return readFixture(t, "issue_list_ready_and_beyond.json"), nil
		case args[0] == "api":
			endpoint := strings.TrimSuffix(args[len(args)-1], "/dependencies/blocked_by")
			number := endpoint[strings.LastIndex(endpoint, "/")+1:]
			if fixture, ok := blockedBy[number]; ok {
				return readFixture(t, fixture), nil
			}
		}
		return nil, fmt.Errorf("no recorded output for gh %s", strings.Join(args, " "))
	}
}

func recordedTracker(t *testing.T, byFeature map[string][]tracker.Ticket) func(args []string) ([]byte, error) {
	t.Helper()
	encode := func(v any) []byte {
		out, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("encode recorded output: %v", err)
		}
		return out
	}
	return func(args []string) ([]byte, error) {
		switch args[0] {
		case "label":
			labels := []map[string]string{{"name": "bug"}}
			for feature := range byFeature {
				labels = append(labels, map[string]string{"name": feature})
			}
			return encode(labels), nil
		case "issue":
			feature := args[slices.Index(args, "--label")+1]
			issues := []map[string]any{}
			for _, ticket := range byFeature[feature] {
				labels := []map[string]string{{"name": feature}}
				if ticket.Status != "" {
					labels = append(labels, map[string]string{"name": "status:" + ticket.Status})
				}
				issues = append(issues, map[string]any{
					"number": ticket.Number, "title": ticket.Title, "body": ticket.Body, "url": ticket.URL, "labels": labels,
				})
			}
			return encode(issues), nil
		case "api":
			deps := []map[string]string{{"html_url": "https://github.com/o/r/issues/99", "state": "closed"}}
			for _, tickets := range byFeature {
				for _, ticket := range tickets {
					if strings.HasSuffix(args[len(args)-1], fmt.Sprintf("/issues/%d/dependencies/blocked_by", ticket.Number)) {
						for _, url := range ticket.BlockedBy {
							deps = append(deps, map[string]string{"html_url": url, "state": "open"})
						}
					}
				}
			}
			return encode(deps), nil
		}
		return nil, fmt.Errorf("no recorded output for gh %s", strings.Join(args, " "))
	}
}

func TestGithubSource(t *testing.T) {
	t.Parallel()
	trackertest.RunContract(t, func(t *testing.T, byFeature map[string][]tracker.Ticket) tracker.Source {
		t.Helper()
		return sourceReplaying(t, recordedTracker(t, byFeature))
	})
}

func TestGithubSource_Features(t *testing.T) {
	t.Parallel()

	t.Run("only project labels are features", func(t *testing.T) {
		t.Parallel()
		got, err := sourceReplaying(t, recordedGitHub(t)).Features(t.Context())
		if err != nil {
			t.Fatalf("Features() error = %v", err)
		}
		want := []tracker.Feature{"project:repo-and-ticket-model", "project:fleet-view"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Features() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("not json", func(t *testing.T) {
		t.Parallel()
		src := sourceReplaying(t, func([]string) ([]byte, error) { return []byte("not json"), nil })
		if _, err := src.Features(t.Context()); err == nil {
			t.Error("Features(not json) error = nil, want one")
		}
	})

	t.Run("a full page is refused as truncated", func(t *testing.T) {
		t.Parallel()
		page := "[" + strings.Repeat(`{"name":"project:x"},`, gh.ListLimit-1) + `{"name":"project:y"}]`
		src := sourceReplaying(t, func([]string) ([]byte, error) { return []byte(page), nil })

		_, err := src.Features(t.Context())
		if full, ok := errors.AsType[*gh.FullPageError](err); !ok || full.Limit != gh.ListLimit {
			t.Errorf("Features(full page) error = %v, want *FullPageError at limit %d", err, gh.ListLimit)
		}
	})
}

func TestGithubSource_Tickets(t *testing.T) {
	t.Parallel()

	t.Run("reads status labels and open blockers", func(t *testing.T) {
		t.Parallel()
		got, err := sourceReplaying(t, recordedGitHub(t)).Tickets(t.Context(), "project:repo-and-ticket-model")
		if err != nil {
			t.Fatalf("Tickets() error = %v", err)
		}
		want := []tracker.Ticket{
			{
				URL: "https://github.com/O-Marsters-1997/command-center/issues/120", Number: 120,
				Title: "Add the tracker package", Body: "Owns everything about reading an issue tracker.",
				Status: "ready", BlockedBy: []string{},
			},
			{
				URL: "https://github.com/O-Marsters-1997/command-center/issues/121", Number: 121,
				Title: "Wire the import route", Body: "Renders the tracker groups available.",
				Status: "in-progress", BlockedBy: []string{},
			},
			{
				URL: "https://github.com/O-Marsters-1997/command-center/issues/122", Number: 122,
				Title: "Sketch a future idea", Body: "Not scoped yet.",
				Status: "backlog", BlockedBy: []string{},
			},
			{
				URL: "https://github.com/O-Marsters-1997/command-center/issues/123", Number: 123,
				Title: "Uncategorised ticket", Body: "No status label at all.",
				BlockedBy: []string{},
			},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Tickets() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("closed blockers are dropped", func(t *testing.T) {
		t.Parallel()
		issue := []byte(`[{"number":105,"title":"t","body":"","url":"u","labels":[]}]`)
		recorded := recordedGitHub(t)
		src := sourceReplaying(t, func(args []string) ([]byte, error) {
			if args[0] == "issue" {
				return issue, nil
			}
			return recorded(args)
		})

		got, err := src.Tickets(t.Context(), "project:x")
		if err != nil {
			t.Fatalf("Tickets() error = %v", err)
		}
		want := []tracker.Ticket{{
			URL: "u", Number: 105, Title: "t",
			BlockedBy: []string{"https://github.com/O-Marsters-1997/command-center/issues/104"},
		}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Tickets() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("reads every blocked_by page", func(t *testing.T) {
		t.Parallel()
		src := sourceReplaying(t, func(args []string) ([]byte, error) {
			if args[0] == "issue" {
				return []byte(`[{"number":7,"title":"t","body":"","url":"u","labels":[]}]`), nil
			}
			if !slices.Contains(args, "--paginate") {
				return []byte(`[{"html_url":"https://x/1","state":"open"}]`), nil
			}
			return []byte(`[{"html_url":"https://x/1","state":"open"}][{"html_url":"https://x/2","state":"open"}]`), nil
		})

		got, err := src.Tickets(t.Context(), "project:x")
		if err != nil {
			t.Fatalf("Tickets() error = %v", err)
		}
		want := []tracker.Ticket{{URL: "u", Number: 7, Title: "t", BlockedBy: []string{"https://x/1", "https://x/2"}}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Tickets() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		issue := []byte(`[{"number":7,"title":"t","body":"","url":"u","labels":[]}]`)
		tests := []struct {
			name      string
			blockedBy []byte
		}{
			{name: "empty blocked_by output", blockedBy: nil},
			{name: "blocked_by not json", blockedBy: []byte("not json")},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				src := sourceReplaying(t, func(args []string) ([]byte, error) {
					if args[0] == "issue" {
						return issue, nil
					}
					return tt.blockedBy, nil
				})
				if _, err := src.Tickets(t.Context(), "project:x"); err == nil {
					t.Errorf("Tickets(blocked_by %q) error = nil, want one", tt.blockedBy)
				}
			})
		}
	})
}
