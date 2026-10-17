package view_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestLineOf(t *testing.T) {
	t.Parallel()

	t.Run("shape by kind", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			kind agentlog.Kind
			want view.LineShape
		}{
			{kind: agentlog.Say, want: view.LineSay},
			{kind: agentlog.Skill, want: view.LineSkill},
			{kind: agentlog.Record, want: view.LineRecord},
			{kind: agentlog.Commit, want: view.LineCall},
			{kind: agentlog.Cmd, want: view.LineCall},
			{kind: agentlog.Fail, want: view.LineCall},
			{kind: agentlog.Tool, want: view.LineCall},
		}
		for _, tt := range tests {
			t.Run(tt.kind.String(), func(t *testing.T) {
				t.Parallel()
				if got := view.LineOf(agentlog.Event{Kind: tt.kind}).Shape; got != tt.want {
					t.Errorf("LineOf(%s).Shape = %q, want %q", tt.kind, got, tt.want)
				}
			})
		}
	})

	t.Run("flattens a multi-line argument", func(t *testing.T) {
		t.Parallel()
		line := view.LineOf(agentlog.Event{Kind: agentlog.Skill, Tool: "tdd", Detail: "one\ntwo"})
		if line.Arg != "one two" {
			t.Errorf("LineOf(skill).Arg = %q, want %q", line.Arg, "one two")
		}
	})

	t.Run("command result", func(t *testing.T) {
		t.Parallel()
		type result struct {
			Result             string
			Failed, Fold, Open bool
		}
		tests := []struct {
			name  string
			event agentlog.Event
			want  result
		}{
			{
				name:  "a pass shows a tick and its duration, output folded",
				event: agentlog.Event{Kind: agentlog.Cmd, Done: true, Elapsed: 1200 * time.Millisecond, Output: "ok"},
				want:  result{Result: "✓ 1.2s", Fold: true},
			},
			{
				name:  "a failure shows its exit code, output open",
				event: agentlog.Event{Kind: agentlog.Cmd, Done: true, ExitCode: 2, Elapsed: 3 * time.Second, Output: "boom"},
				want:  result{Result: "exit 2 3s", Failed: true, Fold: true, Open: true},
			},
			{
				name:  "interrupted is not a pass",
				event: agentlog.Event{Kind: agentlog.Cmd, Done: true, Interrupted: true},
				want:  result{Result: "interrupted", Failed: true},
			},
			{name: "no result yet is running", event: agentlog.Event{Kind: agentlog.Cmd}, want: result{Result: "running"}},
			{
				name:  "a result with no duration shows none",
				event: agentlog.Event{Kind: agentlog.Cmd, Done: true},
				want:  result{Result: "✓"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				line := view.LineOf(tt.event)
				got := result{Result: line.Result, Failed: line.Failed, Fold: line.Fold, Open: line.Open}
				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("LineOf(%+v) mismatch (-want +got):\n%s", tt.event, diff)
				}
			})
		}
	})

	t.Run("records", func(t *testing.T) {
		t.Parallel()
		type record struct{ Verb, Arg, Href string }
		tests := []struct {
			name  string
			event store.Event
			want  record
		}{
			{
				name:  "a PR opened links the PR",
				event: store.Event{Kind: "pr_opened", Detail: "PR #12 https://github.com/o/r/pull/12"},
				want:  record{Verb: "opened", Arg: "PR #12", Href: "https://github.com/o/r/pull/12"},
			},
			{
				name:  "a launch names the worktree",
				event: store.Event{Kind: "run_launched", Detail: "spawned pid 7 in /w/cc-1 from 0123456789"},
				want:  record{Verb: "launched", Arg: "in /w/cc-1 from base @ 0123456"},
			},
			{
				name:  "a push names the tip and base",
				event: store.Event{Kind: "pushed", Detail: "pushed abc to origin/main"},
				want:  record{Verb: "pushed", Arg: "abc to origin/main"},
			},
			{
				name:  "a disposed run says it finished",
				event: store.Event{Kind: "run_disposed", Detail: "push"},
				want:  record{Verb: "agent run finished", Arg: "push"},
			},
			{
				name:  "checks landed split into result and check name",
				event: store.Event{Kind: "checks_landed", Detail: "Unit tests (go) passed"},
				want:  record{Verb: "passed", Arg: "Unit tests (go)"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				line := view.LineOf(view.RecordOf(tt.event))
				got := record{Verb: line.Verb, Arg: line.Arg, Href: line.Href}
				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("LineOf(RecordOf(%s)) mismatch (-want +got):\n%s", tt.event.Kind, diff)
				}
			})
		}
	})

	t.Run("prose", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name string
			text string
			want []view.ProseBlock
		}{
			{
				name: "paragraphs, headings and lists",
				text: "Implemented **it** with `Parse`.\nStill one paragraph.\n\n## Checks\n- `go test`\n- golden",
				want: []view.ProseBlock{
					{Items: [][]view.ProseSpan{{
						{Text: "Implemented "}, {Text: "it", Strong: true}, {Text: " with "}, {Text: "Parse", Code: true},
						{Text: ". Still one paragraph."},
					}}},
					{Items: [][]view.ProseSpan{{{Text: "Checks", Strong: true}}}},
					{List: true, Items: [][]view.ProseSpan{{{Text: "go test", Code: true}}, {{Text: "golden"}}}},
				},
			},
			{
				name: "an unmatched delimiter stays text",
				text: "a ` b ** c",
				want: []view.ProseBlock{{Items: [][]view.ProseSpan{{{Text: "a `"}, {Text: " b **"}, {Text: " c"}}}}},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got := view.LineOf(agentlog.Event{Kind: agentlog.Say, Detail: tt.text}).Blocks
				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("LineOf(say %q).Blocks mismatch (-want +got):\n%s", tt.text, diff)
				}
			})
		}
	})
}
