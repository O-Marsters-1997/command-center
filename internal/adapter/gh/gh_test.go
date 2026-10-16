package gh_test

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
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/adapter/command"
	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/adapter/gh/ghtest"
)

var errGH = errors.New("gh exited 1")

func fakeGH(respond func(args []string) ([]byte, error)) gh.CLI {
	run := func(_ context.Context, _ time.Duration, _, name string, args ...string) ([]byte, error) {
		if name != "gh" {
			return nil, fmt.Errorf("ran %q, want gh", name)
		}
		return respond(args)
	}
	return gh.CLI{Exec: command.ExecutorFunc(run)}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", name, err)
	}
	return raw
}

func onlyRequestedFields(t *testing.T, raw []byte, args []string) []byte {
	t.Helper()
	i := slices.Index(args, "--json")
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("gh %v: no --json fields", args)
	}
	fields := strings.Split(args[i+1], ",")
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	for _, row := range rows {
		for key := range row {
			if !slices.Contains(fields, key) {
				delete(row, key)
			}
		}
	}
	out, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return out
}

func fullPage(row string) []byte {
	return []byte("[" + strings.Repeat(row+",", gh.ListLimit-1) + row + "]")
}

func at(clock string) time.Time {
	t, err := time.Parse(time.RFC3339, clock)
	if err != nil {
		panic(err)
	}
	return t
}

func recordedOutput(t *testing.T, prs []gh.PR) []byte {
	t.Helper()
	states := map[gh.PRState]string{gh.Open: "OPEN", gh.Merged: "MERGED", gh.Closed: "CLOSED"}
	rows := []map[string]any{}
	for _, pr := range prs {
		labels := []map[string]string{}
		for _, l := range pr.Labels {
			labels = append(labels, map[string]string{"name": l})
		}
		mergedAt := ""
		if !pr.MergedAt.IsZero() {
			mergedAt = pr.MergedAt.Format(time.RFC3339)
		}
		rows = append(rows, map[string]any{
			"number": pr.Number, "headRefName": pr.HeadRef, "headRefOid": pr.HeadOid,
			"baseRefName": pr.BaseRef, "baseRefOid": pr.BaseOid, "isDraft": pr.IsDraft,
			"state": states[pr.State], "author": map[string]string{"login": pr.AuthorLogin},
			"labels": labels, "mergedAt": mergedAt, "statusCheckRollup": []any{},
		})
	}
	out, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("encode recorded output: %v", err)
	}
	return out
}

func TestCLI(t *testing.T) {
	t.Parallel()
	ghtest.RunContract(t, func(t *testing.T, seed ghtest.Seed) gh.Forge {
		t.Helper()
		return fakeGH(func(args []string) ([]byte, error) {
			var out []byte
			switch i := slices.Index(args, "--head"); {
			case args[0] == "issue" && args[1] == "list":
				issues := []map[string]string{}
				for url, title := range seed.Titles {
					issues = append(issues, map[string]string{"url": url, "title": title})
				}
				raw, err := json.Marshal(issues)
				if err != nil {
					t.Fatalf("encode recorded issues: %v", err)
				}
				out = raw
			case args[0] == "pr" && args[1] == "list" && i >= 0:
				match := slices.DeleteFunc(slices.Clone(seed.PRs), func(pr gh.PR) bool { return pr.HeadRef != args[i+1] })
				out = recordedOutput(t, match)
			case args[0] == "pr" && args[1] == "list":
				open := slices.DeleteFunc(slices.Clone(seed.PRs), func(pr gh.PR) bool { return pr.State != gh.Open })
				out = recordedOutput(t, open)
			default:
				return nil, nil
			}
			return onlyRequestedFields(t, out, args), nil
		})
	})
}

func TestCLI_List(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		want    gh.Snapshot
	}{
		{name: "no pull requests", fixture: "no_prs.json", want: gh.Snapshot{ByBranch: map[string]gh.PR{}}},
		{
			name:    "rollup keeps the latest completed run per name and drops nameless checks",
			fixture: "mixed_rollup.json",
			want: gh.Snapshot{ByBranch: map[string]gh.PR{"cc-1-first": {
				Number: 41, HeadRef: "cc-1-first", HeadOid: "3f1a9c0d2b7e4a6f8c1d5e9b0a2c4d6e8f0a1b2c",
				BaseRef: "main", BaseOid: "9b0a2c4d6e8f0a1b2c3f1a9c0d2b7e4a6f8c1d5e",
				AuthorLogin: "O-Marsters-1997", State: gh.Open,
				Checks: map[string]gh.CheckState{
					"Tests": {
						Name: "Tests", Status: "COMPLETED", Conclusion: "SUCCESS",
						DetailsURL: "https://github.com/o/r/actions/runs/3", StartedAt: at("2026-08-20T10:10:00Z"),
					},
					"ci/legacy": {
						Name: "ci/legacy", Status: "COMPLETED", Conclusion: "SUCCESS",
						DetailsURL: "https://ci.example.com/legacy", StartedAt: at("2026-08-20T10:01:00Z"),
					},
				},
			}}},
		},
		{
			name:    "draft with an empty rollup",
			fixture: "empty_rollup.json",
			want: gh.Snapshot{ByBranch: map[string]gh.PR{"cc-2-second": {
				Number: 42, HeadRef: "cc-2-second", HeadOid: "1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d",
				BaseRef: "main", BaseOid: "9b0a2c4d6e8f0a1b2c3f1a9c0d2b7e4a6f8c1d5e",
				AuthorLogin: "O-Marsters-1997", IsDraft: true, State: gh.Open,
			}}},
		},
		{
			name:    "merged carries its merge time",
			fixture: "merged.json",
			want: gh.Snapshot{ByBranch: map[string]gh.PR{"cc-0-earlier": {
				Number: 40, HeadRef: "cc-0-earlier", HeadOid: "aa11bb22cc33dd44ee55ff6677889900aabbccdd",
				BaseRef: "main", State: gh.Merged, MergedAt: at("2026-08-20T10:15:00Z"),
			}}},
		},
		{
			name:    "labels",
			fixture: "stacked_ready_to_merge.json",
			want: gh.Snapshot{ByBranch: map[string]gh.PR{"cc-2-second": {
				Number: 43, HeadRef: "cc-2-second", HeadOid: "3f1a9c0d2b7e4a6f8c1d5e9b0a2c4d6e8f0a1b2c",
				BaseRef: "cc-1-first", BaseOid: "9b0a2c4d6e8f0a1b2c3f1a9c0d2b7e4a6f8c1d5e",
				AuthorLogin: "O-Marsters-1997", State: gh.Open, Labels: []string{"keep-open", "ready-to-merge"},
			}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := readFixture(t, tt.fixture)
			cli := fakeGH(func(args []string) ([]byte, error) { return onlyRequestedFields(t, raw, args), nil })

			got, err := cli.List(t.Context(), "/repo", nil)
			if err != nil {
				t.Fatalf("List(%s) error = %v", tt.fixture, err)
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("List(%s) mismatch (-want +got):\n%s", tt.fixture, diff)
			}
		})
	}

	t.Run("a tracked branch missing from the open list is read by head", func(t *testing.T) {
		t.Parallel()
		merged := readFixture(t, "merged.json")
		cli := fakeGH(func(args []string) ([]byte, error) {
			if slices.Contains(args, "--head") && slices.Contains(args, "cc-0-earlier") {
				return onlyRequestedFields(t, merged, args), nil
			}
			return []byte("[]"), nil
		})

		got, err := cli.List(t.Context(), "/repo", []string{"cc-0-earlier", "cc-9-never-opened"})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		want := gh.Snapshot{ByBranch: map[string]gh.PR{"cc-0-earlier": {
			Number: 40, HeadRef: "cc-0-earlier", HeadOid: "aa11bb22cc33dd44ee55ff6677889900aabbccdd",
			BaseRef: "main", State: gh.Merged, MergedAt: at("2026-08-20T10:15:00Z"),
		}}}
		if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("List() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name string
			out  []byte
			err  error
		}{
			{name: "unknown state", out: []byte(`[{"number":1,"state":"WEIRD"}]`)},
			{name: "malformed mergedAt", out: []byte(`[{"number":1,"state":"MERGED","mergedAt":"yesterday"}]`)},
			{name: "not json", out: []byte("not json")},
			{name: "gh fails", err: errGH},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				cli := fakeGH(func([]string) ([]byte, error) { return tt.out, tt.err })

				if _, err := cli.List(t.Context(), "/repo", nil); err == nil {
					t.Errorf("List(%q) error = nil, want one", tt.out)
				}
			})
		}
	})

	t.Run("a full page is refused as truncated", func(t *testing.T) {
		t.Parallel()
		page := fullPage(`{"number":1,"state":"OPEN","headRefName":"b"}`)
		cli := fakeGH(func([]string) ([]byte, error) { return page, nil })

		_, err := cli.List(t.Context(), "/repo", nil)
		if full, ok := errors.AsType[*gh.FullPageError](err); !ok || full.Limit != gh.ListLimit {
			t.Errorf("List(full page) error = %v, want *FullPageError at limit %d", err, gh.ListLimit)
		}
	})
}

func TestCLI_IssueTitles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  []byte
		want map[string]string
	}{
		{name: "no issues", raw: []byte("[]"), want: map[string]string{}},
		{
			name: "keyed on the issue url, not the number",
			raw:  readFixture(t, "issue_list.json"),
			want: map[string]string{
				"https://github.com/owner/repo/issues/100": "Put each ticket's issue title on its row",
				"https://github.com/owner/repo/issues/101": "Give the board its own route",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cli := fakeGH(func(args []string) ([]byte, error) { return onlyRequestedFields(t, tt.raw, args), nil })

			got, err := cli.IssueTitles(t.Context(), "/repo")
			if err != nil {
				t.Fatalf("IssueTitles() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("IssueTitles() mismatch (-want +got):\n%s", diff)
			}
		})
	}

	t.Run("not json", func(t *testing.T) {
		t.Parallel()
		cli := fakeGH(func([]string) ([]byte, error) { return []byte("not json"), nil })

		if _, err := cli.IssueTitles(t.Context(), "/repo"); err == nil {
			t.Error("IssueTitles(not json) error = nil, want one")
		}
	})

	t.Run("a full page is refused as truncated", func(t *testing.T) {
		t.Parallel()
		page := fullPage(`{"title":"t","url":"u"}`)
		cli := fakeGH(func([]string) ([]byte, error) { return page, nil })

		_, err := cli.IssueTitles(t.Context(), "/repo")
		if full, ok := errors.AsType[*gh.FullPageError](err); !ok || full.Limit != gh.ListLimit {
			t.Errorf("IssueTitles(full page) error = %v, want *FullPageError at limit %d", err, gh.ListLimit)
		}
	})
}

func TestPRState_String(t *testing.T) {
	t.Parallel()

	var zero gh.PRState
	if got := zero.String(); got != "absent" {
		t.Errorf("PRState(0).String() = %q, want %q", got, "absent")
	}
}

func FuzzCLI_List(f *testing.F) {
	f.Fuzz(func(t *testing.T, raw []byte) {
		cli := fakeGH(func([]string) ([]byte, error) { return raw, nil })
		snap, err := cli.List(t.Context(), "/repo", nil)
		if err == nil && snap.ByBranch == nil {
			t.Errorf("List(%q) = nil ByBranch with no error", raw)
		}
	})
}
