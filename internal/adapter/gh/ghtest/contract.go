package ghtest

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
)

const FailedRunURL = "https://github.com/o/r/actions/runs/998877/job/2233"

type Seed struct {
	PRs    []gh.PR
	Titles map[string]string
	Logs   map[string]string
}

func RunContract(t *testing.T, newForge func(t *testing.T, seed Seed) gh.Forge) {
	t.Helper()
	mergedAt := time.Date(2026, 8, 20, 10, 15, 0, 0, time.UTC)
	open := gh.PR{
		Number: 41, HeadRef: "cc-1-first", HeadOid: "aaa", BaseRef: "main", BaseOid: "bbb",
		AuthorLogin: "o", IsDraft: true, State: gh.Open, Labels: []string{"keep-open"},
	}
	merged := gh.PR{
		Number: 40, HeadRef: "cc-2-second", HeadOid: "ccc", BaseRef: "main", State: gh.Merged, MergedAt: mergedAt,
	}
	closed := gh.PR{Number: 39, HeadRef: "cc-3-third", HeadOid: "ddd", BaseRef: "main", State: gh.Closed}

	t.Run("List of an empty forge", func(t *testing.T) {
		t.Parallel()
		got, err := newForge(t, Seed{}).List(t.Context(), "/repo", []string{"cc-1-first"})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if diff := cmp.Diff(gh.Snapshot{ByBranch: map[string]gh.PR{}}, got); diff != "" {
			t.Errorf("List() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("List returns open PRs and the tracked branches' others", func(t *testing.T) {
		t.Parallel()
		forge := newForge(t, Seed{PRs: []gh.PR{open, merged, closed}})

		got, err := forge.List(t.Context(), "/repo", []string{"cc-2-second"})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		want := gh.Snapshot{ByBranch: map[string]gh.PR{"cc-1-first": open, "cc-2-second": merged}}
		if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("List(tracked cc-2-second) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("IssueTitles is keyed on the issue URL", func(t *testing.T) {
		t.Parallel()
		titles := map[string]string{"https://github.com/o/r/issues/1": "First"}

		got, err := newForge(t, Seed{Titles: titles}).IssueTitles(t.Context(), "/repo")
		if err != nil {
			t.Fatalf("IssueTitles() error = %v", err)
		}
		if diff := cmp.Diff(titles, got); diff != "" {
			t.Errorf("IssueTitles() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("FailedLog reads the log of the run a details URL names", func(t *testing.T) {
		t.Parallel()
		forge := newForge(t, Seed{Logs: map[string]string{FailedRunURL: "boom\n"}})

		got, err := forge.FailedLog(t.Context(), "/repo", FailedRunURL)
		if err != nil {
			t.Fatalf("FailedLog() error = %v", err)
		}
		if got != "boom\n" {
			t.Errorf("FailedLog(%q) = %q, want %q", FailedRunURL, got, "boom\n")
		}
	})

	t.Run("FailedLog refuses a URL that names no Actions run", func(t *testing.T) {
		t.Parallel()
		forge := newForge(t, Seed{Logs: map[string]string{FailedRunURL: "boom\n"}})

		if _, err := forge.FailedLog(t.Context(), "/repo", "https://example.com/status/1"); err == nil {
			t.Error("FailedLog(non-Actions URL) error = nil, want an error")
		}
	})

	t.Run("writes succeed", func(t *testing.T) {
		t.Parallel()
		forge := newForge(t, Seed{PRs: []gh.PR{open}})
		ctx := t.Context()

		for name, err := range map[string]error{
			"Create":     forge.Create(ctx, "/wt", "main", "body", true, []string{"cc-protected"}),
			"Ready":      forge.Ready(ctx, "/repo", "cc-1-first"),
			"Edit":       forge.Edit(ctx, "/repo", "cc-1-first", "main"),
			"Comment":    forge.Comment(ctx, "/repo", "cc-1-first", "hello"),
			"CloseIssue": forge.CloseIssue(ctx, "/repo", "https://github.com/o/r/issues/1"),
		} {
			if err != nil {
				t.Errorf("%s() error = %v, want nil", name, err)
			}
		}
	})
}
