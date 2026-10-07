package agentlog_test

import (
	"os"
	"path/filepath"
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
							{Kind: agentlog.Tool, Tool: "Bash",
								Detail: "find internal/cc internal/plan -type f | sort"},
							{At: 744 * time.Millisecond, Kind: agentlog.File, Tool: "Read",
								Detail: "/Users/dev/Documents/personal/ai-development/" +
									"command-center-cc-74-grouped-board/internal/cc/group_page_test.go"},
							{At: 753 * time.Millisecond, Kind: agentlog.Pass, Detail: "1\tpackage cc_test"},
							{At: 1328 * time.Millisecond, Kind: agentlog.Pass,
								Detail: "internal/cc/app_test.go"},
						},
					},
					{
						Skill: "go-idiomatic", At: 237445 * time.Millisecond,
						Events: []agentlog.Event{
							{At: 237451 * time.Millisecond, Kind: agentlog.Pass,
								Detail: "Launching skill: go-idiomatic"},
							{At: 763225 * time.Millisecond, Kind: agentlog.Tool, Tool: "Bash",
								Detail: "type go; type rtk 2>/dev/null; alias go 2>/dev/null"},
							{At: 765702 * time.Millisecond, Kind: agentlog.Fail, Detail: "Exit code 1"},
						},
					},
					{
						Skill: "clean-comments",
						Note: "internal/cc/export_test.go internal/cc/group_page_test.go " +
							"internal/cc/server.go",
						At: 1572197 * time.Millisecond,
						Events: []agentlog.Event{
							{At: 1572209 * time.Millisecond, Kind: agentlog.Pass,
								Detail: "Launching skill: clean-comments"},
						},
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
		{"assistant prose", `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z",` +
			`"message":{"content":[{"type":"text","text":"I will start by reading the plan."}]}}`},
		{"tool_progress", `{"type":"tool_progress","tool_name":"Bash","heartbeat":true}`},
		{"result", `{"type":"result","subtype":"success","duration_ms":1,"num_turns":1}`},
		{"malformed json", `{"type":"assistant"`},
		{"empty", ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got, ok := agentlog.ParseLine([]byte(tt.line)); ok {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := agentlog.ParseLine([]byte(tt.line))
			if !ok {
				t.Fatalf("ParseLine dropped %s", tt.name)
			}
			if got != tt.want {
				t.Errorf("ParseLine = %+v; want %+v", got, tt.want)
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
			if gotPhase.Events[j] != wantEvent {
				t.Errorf("phase %d event %d = %+v; want %+v", i, j, gotPhase.Events[j], wantEvent)
			}
		}
	}
}
