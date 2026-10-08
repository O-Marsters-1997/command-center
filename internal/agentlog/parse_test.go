package agentlog_test

import (
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		want    agentlog.Run
	}{
		{
			name:    "alive run has no result and cuts a phase at the skill",
			fixture: "alive.jsonl",
			want: agentlog.Run{
				Lines: 8,
				Phases: []agentlog.Phase{
					{
						Events: []agentlog.Event{
							{Kind: agentlog.Tool, Tool: "Bash", Detail: "go test ./..."},
							{At: 1500 * time.Millisecond, Kind: agentlog.Fail,
								Detail: "FAIL\tinternal/agentlog"},
						},
					},
					{
						Skill: "tdd", Note: "internal/agentlog", At: 2 * time.Second,
						Events: []agentlog.Event{
							{At: 3 * time.Second, Kind: agentlog.File, Tool: "Edit",
								Detail: "internal/agentlog/parse.go"},
							{At: 4 * time.Second, Kind: agentlog.Pass, Detail: "edited"},
						},
					},
				},
			},
		},
		{
			name:    "a run of only dropped kinds has no phases",
			fixture: "dropped.jsonl",
			want:    agentlog.Run{Lines: 3},
		},
		{
			name:    "a truncated final line parses as the lines before it",
			fixture: "truncated.jsonl",
			want: agentlog.Run{
				Lines: 2,
				Phases: []agentlog.Phase{{
					Events: []agentlog.Event{
						{Kind: agentlog.Tool, Tool: "Bash", Detail: "just test"},
						{At: time.Second, Kind: agentlog.Pass, Detail: "ok"},
					},
				}},
			},
		},
		{
			name:    "two results report the last one",
			fixture: "run27.jsonl",
			want: agentlog.Run{
				Lines: 23,
				Result: &agentlog.Result{
					Outcome: "success", Duration: 561464 * time.Millisecond,
					Turns: 42, CostUSD: 8.288799200000001,
				},
				Phases: []agentlog.Phase{
					{
						Events: []agentlog.Event{
							{Kind: agentlog.Say, Detail: "I'll start by exploring the existing code structure — " +
								"the `internal/cc` package, the `plan` package, and the untracked test file already present."},
							{At: 1099 * time.Millisecond, Kind: agentlog.Tool, Tool: "Bash",
								Detail: "find internal/cc internal/plan -type f | sort"},
							{At: 1843 * time.Millisecond, Kind: agentlog.File, Tool: "Read",
								Detail: "/Users/dev/Documents/personal/ai-development/" +
									"command-center-cc-74-grouped-board/internal/cc/group_page_test.go"},
							{At: 1852 * time.Millisecond, Kind: agentlog.Pass, Detail: "1\tpackage cc_test"},
							{At: 2427 * time.Millisecond, Kind: agentlog.Pass,
								Detail: "internal/cc/app_test.go"},
						},
					},
					{
						Skill: "go-idiomatic", At: 238544 * time.Millisecond,
						Events: []agentlog.Event{
							{At: 764324 * time.Millisecond, Kind: agentlog.Tool, Tool: "Bash",
								Detail: "type go; type rtk 2>/dev/null; alias go 2>/dev/null"},
							{At: 766801 * time.Millisecond, Kind: agentlog.Fail, Detail: "Exit code 1"},
						},
					},
					{
						Skill: "clean-comments",
						Note: "internal/cc/export_test.go internal/cc/group_page_test.go " +
							"internal/cc/server.go",
						At: 1573296 * time.Millisecond,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, err := os.Open(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatalf("open fixture: %v", err)
			}
			defer func() { _ = f.Close() }()

			got, err := agentlog.Parse(f)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			assertRun(t, got, tt.want)
		})
	}
}

func TestParseLineDropsUnrenderableKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
	}{
		{"system", `{"type":"system","subtype":"init","session_id":"s"}`},
		{"rate_limit_event", `{"type":"rate_limit_event","session_id":"s"}`},
		{"thinking", `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z",` +
			`"message":{"content":[{"type":"thinking","thinking":"hmm"}]}}`},
		{"blank prose", `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z",` +
			`"message":{"content":[{"type":"text","text":"  \n"}]}}`},
		{"a subagent's prose", `{"type":"assistant","parent_tool_use_id":"toolu_1",` +
			`"message":{"content":[{"type":"text","text":"subagent thinking aloud"}]}}`},
		{"tool_progress", `{"type":"tool_progress","tool_name":"Bash","heartbeat":true}`},
		{"result", `{"type":"result","subtype":"success","duration_ms":1,"num_turns":1}`},
		{"malformed json", `{"type":"assistant"`},
		{"empty", ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := agentlog.ParseLine([]byte(tt.line)); len(got) != 0 {
				t.Errorf("ParseLine(%s) kept %+v; want dropped", tt.name, got)
			}
		})
	}
}

func TestParseLineKeeps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want agentlog.Event
	}{
		{
			name: "skill carries its slug and args",
			line: `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
				`[{"type":"tool_use","name":"Skill","input":{"skill":"tdd","args":"./x"}}]}}`,
			want: agentlog.Event{Kind: agentlog.Skill, Tool: "tdd", Detail: "./x"},
		},
		{
			name: "a thinking block ahead of a tool use does not hide it",
			line: `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
				`[{"type":"thinking","thinking":"hmm"},` +
				`{"type":"tool_use","name":"Bash","input":{"command":"just test"}}]}}`,
			want: agentlog.Event{Kind: agentlog.Tool, Tool: "Bash", Detail: "just test"},
		},
		{
			name: "a write is a file event",
			line: `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
				`[{"type":"tool_use","name":"Write","input":{"file_path":"a.go","content":"x"}}]}}`,
			want: agentlog.Event{Kind: agentlog.File, Tool: "Write", Detail: "a.go"},
		},
		{
			name: "a grep renders its pattern",
			line: `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
				`[{"type":"tool_use","name":"Grep","input":{"pattern":"func Parse","output_mode":"content"}}]}}`,
			want: agentlog.Event{Kind: agentlog.Tool, Tool: "Grep", Detail: "func Parse"},
		},
		{
			name: "a tool with no known primary input renders bare",
			line: `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
				`[{"type":"tool_use","name":"ListAgents","input":{}}]}}`,
			want: agentlog.Event{Kind: agentlog.Tool, Tool: "ListAgents"},
		},
		{
			name: "assistant prose is a say event",
			line: `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
				`[{"type":"text","text":"I will start by reading the plan.\n"}]}}`,
			want: agentlog.Event{Kind: agentlog.Say, Detail: "I will start by reading the plan."},
		},
		{
			name: "a call carries its tool_use id",
			line: `{"type":"assistant","message":{"content":` +
				`[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}`,
			want: agentlog.Event{Kind: agentlog.Tool, Tool: "Bash", Detail: "ls", CallID: "toolu_1"},
		},
		{
			name: "a result keeps every text block and the id of the call it answers",
			line: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1",` +
				`"is_error":true,"content":[{"type":"text","text":"FAIL x"},{"type":"text","text":"exit 1\n"}]}]}}`,
			want: agentlog.Event{Kind: agentlog.Fail, Detail: "FAIL x", CallID: "toolu_1",
				Output: "FAIL x\nexit 1", OutputLines: 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := agentlog.ParseLine([]byte(tt.line))
			if len(got) != 1 {
				t.Fatalf("ParseLine(%s) = %+v; want one event", tt.name, got)
			}
			if !sameEvent(got[0], tt.want) {
				t.Errorf("ParseLine = %+v; want %+v", got[0], tt.want)
			}
		})
	}
}

func assertRun(t *testing.T, got, want agentlog.Run) {
	t.Helper()

	if got.Lines != want.Lines {
		t.Errorf("Lines = %d; want %d", got.Lines, want.Lines)
	}
	switch {
	case want.Result == nil && got.Result != nil:
		t.Errorf("Result = %+v; want nil", got.Result)
	case want.Result != nil && got.Result == nil:
		t.Errorf("Result = nil; want %+v", want.Result)
	case want.Result != nil && *got.Result != *want.Result:
		t.Errorf("Result = %+v; want %+v", *got.Result, *want.Result)
	}
	if len(got.Phases) != len(want.Phases) {
		t.Fatalf("%d phases; want %d (%+v)", len(got.Phases), len(want.Phases), got.Phases)
	}
	for i, wantPhase := range want.Phases {
		gotPhase := got.Phases[i]
		if gotPhase.Skill != wantPhase.Skill || gotPhase.Note != wantPhase.Note || gotPhase.At != wantPhase.At {
			t.Errorf("phase %d = {%q %q %v}; want {%q %q %v}", i,
				gotPhase.Skill, gotPhase.Note, gotPhase.At,
				wantPhase.Skill, wantPhase.Note, wantPhase.At)
		}
		if len(gotPhase.Events) != len(wantPhase.Events) {
			t.Errorf("phase %d has %d events; want %d (%+v)",
				i, len(gotPhase.Events), len(wantPhase.Events), gotPhase.Events)
			continue
		}
		for j, wantEvent := range wantPhase.Events {
			if !sameReading(gotPhase.Events[j], wantEvent) {
				t.Errorf("phase %d event %d = %+v; want %+v", i, j, gotPhase.Events[j], wantEvent)
			}
		}
	}
}

func sameReading(got, want agentlog.Event) bool {
	return got.At == want.At && got.Kind == want.Kind && got.Tool == want.Tool && got.Detail == want.Detail
}

func sameEvent(got, want agentlog.Event) bool {
	return sameReading(got, want) && got.CallID == want.CallID && got.Output == want.Output &&
		got.OutputLines == want.OutputLines && sameDiff(got.Diff, want.Diff)
}

func sameDiff(got, want agentlog.Diff) bool {
	return got.Added == want.Added && got.Removed == want.Removed && slices.Equal(got.Lines, want.Lines)
}

func TestParseLineWalksEveryContentBlock(t *testing.T) {
	t.Parallel()

	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"Reading both."},` +
		`{"type":"tool_use","name":"Read","input":{"file_path":"a.go"}},` +
		`{"type":"tool_use","name":"Read","input":{"file_path":"b.go"}}]}}`

	got := agentlog.ParseLine([]byte(line))
	want := []agentlog.Event{
		{Kind: agentlog.Say, Detail: "Reading both."},
		{Kind: agentlog.File, Tool: "Read", Detail: "a.go"},
		{Kind: agentlog.File, Tool: "Read", Detail: "b.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("ParseLine = %+v; want %+v", got, want)
	}
	for i := range want {
		if !sameEvent(got[i], want[i]) {
			t.Errorf("event %d = %+v; want %+v", i, got[i], want[i])
		}
	}
}

func TestParseLineBoundsAResultsOutputButCountsItWhole(t *testing.T) {
	t.Parallel()

	output := strings.Repeat("row\\n", 99) + "row"
	line := `{"type":"user","message":{"content":[{"type":"tool_result","content":"` + output + `"}]}}`

	got := agentlog.ParseLine([]byte(line))
	if len(got) != 1 {
		t.Fatalf("ParseLine = %+v; want one event", got)
	}
	if got[0].OutputLines != 100 {
		t.Errorf("OutputLines = %d; want 100", got[0].OutputLines)
	}
	if kept := strings.Count(got[0].Output, "\n") + 1; kept != 40 {
		t.Errorf("Output keeps %d lines; want 40", kept)
	}
}

func TestParseLineDiffsAnEdit(t *testing.T) {
	t.Parallel()

	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":` +
		`{"file_path":"a.go","old_string":"a\nb\nc\nd","new_string":"a\nb\nC\nC2\nd"}}]}}`

	got := agentlog.ParseLine([]byte(line))
	if len(got) != 1 {
		t.Fatalf("ParseLine = %+v; want one event", got)
	}
	want := agentlog.Diff{Added: 2, Removed: 1, Lines: []agentlog.DiffLine{
		{Op: agentlog.Keep, Text: "a"},
		{Op: agentlog.Keep, Text: "b"},
		{Op: agentlog.Remove, Text: "c"},
		{Op: agentlog.Add, Text: "C"},
		{Op: agentlog.Add, Text: "C2"},
		{Op: agentlog.Keep, Text: "d"},
	}}
	if !sameDiff(got[0].Diff, want) {
		t.Errorf("Diff = %+v; want %+v", got[0].Diff, want)
	}
}

func TestParseCountsEachPhasesTurnsAndPricesItsSpend(t *testing.T) {
	t.Parallel()

	f, err := os.Open(filepath.Join("testdata", "alive.jsonl"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	run, err := agentlog.Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(run.Phases) != 2 {
		t.Fatalf("%d phases; want 2", len(run.Phases))
	}

	r1 := agentlog.Weight(agentlog.RequestUsage{Input: 10, CacheCreate: 20, CacheRead: 30, Output: 5})
	r2 := agentlog.Weight(agentlog.RequestUsage{Input: 1, CacheCreate: 2, CacheRead: 3, Output: 4})
	r3 := agentlog.Weight(agentlog.RequestUsage{Output: 7})
	for i, want := range []agentlog.Phase{{Turns: 1, Spend: r1}, {Turns: 2, Spend: r2 + r3}} {
		got := run.Phases[i]
		if got.Turns != want.Turns || math.Abs(got.Spend-want.Spend) > 1e-12 {
			t.Errorf("phase %d = %d turns, $%g; want %d turns, $%g", i, got.Turns, got.Spend, want.Turns, want.Spend)
		}
	}
	if run.End != 4*time.Second {
		t.Errorf("End = %v; want 4s, the last kept event", run.End)
	}
}
