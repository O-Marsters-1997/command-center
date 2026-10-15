package agentlog_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

func parseString(t *testing.T, log string) agentlog.Run {
	t.Helper()
	run, err := agentlog.Parse(strings.NewReader(log))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return run
}

func parseFixture(t *testing.T, name string) agentlog.Run {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", name, err)
	}
	return parseString(t, string(body))
}

func tailAll(log string) []agentlog.Event {
	var tail agentlog.Tail
	var events []agentlog.Event
	for line := range strings.SplitSeq(strings.TrimRight(log, "\n"), "\n") {
		events = append(events, tail.Read([]byte(line))...)
	}
	return events
}

func eventsOf(run agentlog.Run) []agentlog.Event {
	var events []agentlog.Event
	for _, phase := range run.Phases {
		events = append(events, phase.Events...)
	}
	return events
}

func cmdsOf(run agentlog.Run) []agentlog.Event {
	var cmds []agentlog.Event
	for _, e := range eventsOf(run) {
		if e.Kind == agentlog.Cmd {
			cmds = append(cmds, e)
		}
	}
	return cmds
}

func bashRun(t *testing.T, command string, failed bool) string {
	t.Helper()
	input, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}
	result := `{"type":"tool_result","tool_use_id":"c","content":"ok"}`
	if failed {
		result = `{"type":"tool_result","tool_use_id":"c","is_error":true,"content":"Exit code 1\nhook failed"}`
	}
	return `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
		`[{"type":"tool_use","id":"c","name":"Bash","input":` + string(input) + `}]}}` + "\n" +
		`{"type":"user","timestamp":"2026-01-01T00:00:01Z","message":{"content":[` + result + `]}}` + "\n"
}

type commitCase struct {
	name    string
	command string
	failed  bool
	kind    agentlog.Kind
	detail  string
}

var commitCases = []commitCase{
	{"double-quoted message", `git commit -m "fix the thing"`, false, agentlog.Commit, "fix the thing"},
	{"single-quoted message", `git add . && git commit -am 'wip: more'`, false, agentlog.Commit, "wip: more"},
	{"first line of a multi-line message", "git commit -m \"title\n\nbody\"", false, agentlog.Commit, "title"},
	{"heredoc has no message", "git commit -m \"$(cat <<'EOF'\ntitle\nEOF\n)\"", false, agentlog.Commit, ""},
	{"commit-tree is not a commit", `git commit-tree abc`, false, agentlog.Cmd, `git commit-tree abc`},
	{"quoted mention is not a commit", `echo 'x git commit'`, false, agentlog.Cmd, `echo 'x git commit'`},
	{"dry run is not a commit", `git commit --dry-run`, false, agentlog.Cmd, `git commit --dry-run`},
	{"git -C dir commit", `git -C /w commit -m "in dir"`, false, agentlog.Commit, "in dir"},
	{"file has no message", `git commit -F msg.txt`, false, agentlog.Commit, ""},
	{"failed commit stays a command", `git commit -m "nope"`, true, agentlog.Cmd, `git commit -m "nope"`},
	{"other git commands stay commands", `git status`, false, agentlog.Cmd, `git status`},
}

func TestParse(t *testing.T) {
	t.Parallel()

	findOutput, err := os.ReadFile("testdata/run27.find-output.golden")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

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
				Start: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC),
				End:   4 * time.Second,
				Phases: []agentlog.Phase{
					{
						Turns: 1, Spend: 0.0004992,
						Events: []agentlog.Event{
							{Kind: agentlog.Cmd, Tool: "Bash", Detail: "go test ./...",
								Output: "FAIL\tinternal/agentlog\nno such package", OutputLines: 2,
								Done: true, ExitCode: 1, Elapsed: 1500 * time.Millisecond},
						},
					},
					{
						Skill: "tdd", Note: "internal/agentlog", At: 2 * time.Second,
						Turns: 2, Spend: 0.00038592,
						Events: []agentlog.Event{
							{At: 3 * time.Second, Kind: agentlog.File, Tool: "Edit",
								Detail: "internal/agentlog/parse.go"},
							{At: 4 * time.Second, Kind: agentlog.Pass, Detail: "edited",
								Output: "edited\nok", OutputLines: 2},
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
				Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				End:   time.Second,
				Phases: []agentlog.Phase{{
					Turns: 1, Spend: 6.4e-05,
					Events: []agentlog.Event{
						{Kind: agentlog.Cmd, Tool: "Bash", Detail: "just test",
							Output: "ok", OutputLines: 1, Done: true, Elapsed: time.Second},
					},
				}},
			},
		},
		{
			name:    "two results report the last one",
			fixture: "run27.jsonl",
			want: agentlog.Run{
				Lines: 23,
				Start: time.Date(2026, 8, 25, 10, 56, 4, 185*int(time.Millisecond), time.UTC),
				End:   1573296 * time.Millisecond,
				Result: &agentlog.Result{
					Outcome: "success", Duration: 561464 * time.Millisecond,
					Turns: 42, CostUSD: 8.288799200000001,
				},
				Phases: []agentlog.Phase{
					{
						Turns: 1, Spend: 0.38171008,
						Events: []agentlog.Event{
							{Kind: agentlog.Say, Detail: "I'll start by exploring the existing code structure — " +
								"the `internal/cc` package, the `plan` package, and the untracked test file already present."},
							{At: 1099 * time.Millisecond, Kind: agentlog.Cmd, Tool: "Bash",
								Detail: "find internal/cc internal/plan -type f | sort",
								CallID: "toolu_01TFGB4Saexqc9Ctwccjqo8U", Output: string(findOutput), OutputLines: 93,
								Done: true, Elapsed: 1328 * time.Millisecond},
							{At: 1843 * time.Millisecond, Kind: agentlog.File, Tool: "Read",
								Detail: "/Users/dev/Documents/personal/ai-development/" +
									"command-center-cc-74-grouped-board/internal/cc/group_page_test.go",
								CallID: "toolu_01SVRG41A6CCGzYD3fFb7Emi"},
							{At: 1852 * time.Millisecond, Kind: agentlog.Pass, Detail: "1\tpackage cc_test",
								CallID: "toolu_01SVRG41A6CCGzYD3fFb7Emi", Output: "1\tpackage cc_test\n2\t", OutputLines: 2},
						},
					},
					{
						Skill: "go-idiomatic", At: 238544 * time.Millisecond,
						Turns: 2, Spend: 0.19796928000000003,
						Events: []agentlog.Event{
							{At: 764324 * time.Millisecond, Kind: agentlog.Cmd, Tool: "Bash",
								Detail: "type go; type rtk 2>/dev/null; alias go 2>/dev/null",
								CallID: "toolu_019SuUKTAJWXx7GiPbakYQSe",
								Output: "go is /opt/homebrew/bin/go\nrtk is /opt/homebrew/bin/rtk", OutputLines: 2,
								Done: true, ExitCode: 1, Elapsed: 2477 * time.Millisecond},
						},
					},
					{
						Skill: "clean-comments",
						Turns: 1, Spend: 0.19517952000000002,
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
			if diff := cmp.Diff(tt.want, parseFixture(t, tt.fixture), cmpopts.EquateApprox(0, 1e-12)); diff != "" {
				t.Errorf("Parse(%s) mismatch (-want +got):\n%s", tt.fixture, diff)
			}
		})
	}

	t.Run("prices each phase's turns", func(t *testing.T) {
		t.Parallel()
		run := parseFixture(t, "alive.jsonl")
		r1 := agentlog.Weight(agentlog.RequestUsage{Input: 10, CacheCreate: 20, CacheRead: 30, Output: 5})
		r2 := agentlog.Weight(agentlog.RequestUsage{Input: 1, CacheCreate: 2, CacheRead: 3, Output: 4})
		r3 := agentlog.Weight(agentlog.RequestUsage{Output: 7})

		type phase struct {
			Turns int
			Spend float64
		}
		var got []phase
		for _, p := range run.Phases {
			got = append(got, phase{p.Turns, p.Spend})
		}
		want := []phase{{Turns: 1, Spend: r1}, {Turns: 2, Spend: r2 + r3}}
		if diff := cmp.Diff(want, got, cmpopts.EquateApprox(0, 1e-12)); diff != "" {
			t.Errorf("Parse(alive.jsonl) phases mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("counts edits and writes from the structured result", func(t *testing.T) {
		t.Parallel()
		got := map[string][2]int{}
		for _, e := range parseFixture(t, "edits.jsonl").Phases[0].Events {
			if e.Kind == agentlog.Pass {
				got[e.CallID] = [2]int{e.Diff.Added, e.Diff.Removed}
			}
		}
		want := map[string][2]int{"toolu_edit": {3, 2}, "toolu_create": {3, 0}, "toolu_update": {3, 1}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Parse(edits.jsonl) result counts mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("files a result under its call's phase", func(t *testing.T) {
		t.Parallel()
		run := parseString(t, `{"type":"assistant","message":{"content":[`+
			`{"type":"tool_use","id":"b","name":"Bash","input":{"command":"ls"}},`+
			`{"type":"tool_use","id":"s","name":"Skill","input":{"skill":"tdd"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b","content":"x"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"s","content":"Launching skill: tdd"}]}}
`)
		want := []agentlog.Phase{
			{Events: []agentlog.Event{{
				Kind: agentlog.Cmd, Tool: "Bash", Detail: "ls", CallID: "b", Done: true, Output: "x", OutputLines: 1,
			}}},
			{Skill: "tdd"},
		}
		diff := cmp.Diff(want, run.Phases, cmpopts.EquateEmpty(), cmpopts.IgnoreFields(agentlog.Phase{}, "Spend"))
		if diff != "" {
			t.Errorf("Parse() phases mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("keeps a subagent's skill and turns out of the main run", func(t *testing.T) {
		t.Parallel()
		run := parseString(t, `{"type":"assistant","request_id":"r1","message":{"content":[`+
			`{"type":"tool_use","id":"task","name":"Task","input":{"prompt":"go"}}]}}
{"type":"assistant","request_id":"r2","parent_tool_use_id":"task","message":{"content":[`+
			`{"type":"tool_use","id":"s","name":"Skill","input":{"skill":"tdd"}}]}}
`)
		want := []agentlog.Phase{{Turns: 1, Events: []agentlog.Event{
			{Kind: agentlog.Tool, Tool: "Task", Detail: "go", CallID: "task"},
			{Kind: agentlog.Tool, Tool: "Skill", Detail: "tdd", CallID: "s"},
		}}}
		if diff := cmp.Diff(want, run.Phases, cmpopts.IgnoreFields(agentlog.Phase{}, "Spend")); diff != "" {
			t.Errorf("Parse() phases mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("run27's commands carry exit code, duration and output", func(t *testing.T) {
		t.Parallel()
		cmds := cmdsOf(parseFixture(t, "run27.jsonl"))
		if len(cmds) != 2 {
			t.Fatalf("Parse(run27.jsonl) Cmd events = %+v, want 2", cmds)
		}
		passed, failed := cmds[0], cmds[1]
		if !passed.Done || passed.ExitCode != 0 || passed.Elapsed <= 0 ||
			!strings.HasPrefix(passed.Output, "internal/cc/app_test.go\ninternal/cc/app.go\n") {
			t.Errorf("Parse(run27.jsonl) first Cmd = %+v, want exit 0, a duration and stdout as output", passed)
		}
		if !failed.Done || failed.ExitCode != 1 || failed.Elapsed <= 0 ||
			failed.Output != "go is /opt/homebrew/bin/go\nrtk is /opt/homebrew/bin/rtk" {
			t.Errorf("Parse(run27.jsonl) second Cmd = %+v, want exit 1, a duration and output without its exit code", failed)
		}
	})

	t.Run("command results", func(t *testing.T) {
		t.Parallel()
		const call = `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
			`[{"type":"tool_use","id":"c","name":"Bash","input":{"command":"make"}}]}}` + "\n"
		result := func(timestamp, block, structured string) string {
			line := `{"type":"user",` + timestamp + `"message":{"content":[` + block + `]}`
			if structured != "" {
				line += `,"tool_use_result":` + structured
			}
			return call + line + "}\n"
		}
		const stamp = `"timestamp":"2026-01-01T00:00:03.5Z",`
		const ok = `{"type":"tool_result","tool_use_id":"c","content":"from content"}`
		const bad = `{"type":"tool_result","tool_use_id":"c","is_error":true,"content":"Exit code 2\nfrom content"}`
		const elapsed = 3500 * time.Millisecond

		tests := []struct {
			name string
			log  string
			want agentlog.Event
		}{
			{
				name: "stdout and stderr join, skipping an empty part",
				log:  result(stamp, ok, `{"stdout":"out","stderr":"","interrupted":false}`),
				want: agentlog.Event{Done: true, Output: "out", OutputLines: 1, Elapsed: elapsed},
			},
			{
				name: "both streams show",
				log:  result(stamp, ok, `{"stdout":"out","stderr":"warn"}`),
				want: agentlog.Event{Done: true, Output: "out\nwarn", OutputLines: 2, Elapsed: elapsed},
			},
			{
				name: "interrupted is not a pass",
				log:  result(stamp, ok, `{"stdout":"","stderr":"","interrupted":true}`),
				want: agentlog.Event{Done: true, Interrupted: true, Output: "from content", OutputLines: 1, Elapsed: elapsed},
			},
			{
				name: "output that merely starts like an exit code is not a failure",
				log:  result(stamp, ok, `{"stdout":"Exit code 3 reached"}`),
				want: agentlog.Event{Done: true, Output: "Exit code 3 reached", OutputLines: 1, Elapsed: elapsed},
			},
			{
				name: "a bare string result carries the exit code",
				log:  result(stamp, bad, `"Error: Exit code 2\nboom"`),
				want: agentlog.Event{Done: true, ExitCode: 2, Output: "boom", OutputLines: 1, Elapsed: elapsed},
			},
			{
				name: "no tool_use_result falls back to the content text",
				log:  result(stamp, ok, ""),
				want: agentlog.Event{Done: true, Output: "from content", OutputLines: 1, Elapsed: elapsed},
			},
			{
				name: "no tool_use_result on a failure reads the exit code from the content",
				log:  result(stamp, bad, ""),
				want: agentlog.Event{Done: true, ExitCode: 2, Output: "from content", OutputLines: 1, Elapsed: elapsed},
			},
			{
				name: "a result with no timestamp has no duration",
				log:  result("", ok, `{"stdout":"out"}`),
				want: agentlog.Event{Done: true, Output: "out", OutputLines: 1},
			},
			{name: "a call never answered is running", log: call},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				want := tt.want
				want.Kind, want.Tool, want.Detail, want.CallID = agentlog.Cmd, "Bash", "make", "c"
				got := cmdsOf(parseString(t, tt.log))
				if diff := cmp.Diff([]agentlog.Event{want}, got, cmpopts.IgnoreFields(agentlog.Event{}, "At")); diff != "" {
					t.Errorf("Parse(%s) Cmd events mismatch (-want +got):\n%s", tt.name, diff)
				}
			})
		}
	})

	t.Run("a successful git commit is a commit event", func(t *testing.T) {
		t.Parallel()
		for _, tt := range commitCases {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got := eventsOf(parseString(t, bashRun(t, tt.command, tt.failed)))
				if len(got) != 1 || got[0].Kind != tt.kind || got[0].Detail != tt.detail {
					t.Errorf("Parse(%q) events = %+v, want one %v with detail %q", tt.command, got, tt.kind, tt.detail)
				}
			})
		}
	})
}

func TestParseLine(t *testing.T) {
	t.Parallel()

	t.Run("drops unrenderable kinds", func(t *testing.T) {
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
					t.Errorf("ParseLine(%s) = %+v, want none", tt.name, got)
				}
			})
		}
	})

	output := strings.Repeat("row\\n", 99) + "row"
	tests := []struct {
		name string
		line string
		want []agentlog.Event
	}{
		{
			name: "skill carries its slug and args",
			line: `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
				`[{"type":"tool_use","name":"Skill","input":{"skill":"tdd","args":"./x"}}]}}`,
			want: []agentlog.Event{{Kind: agentlog.Skill, Tool: "tdd", Detail: "./x"}},
		},
		{
			name: "a thinking block ahead of a tool use does not hide it",
			line: `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
				`[{"type":"thinking","thinking":"hmm"},` +
				`{"type":"tool_use","name":"Bash","input":{"command":"just test"}}]}}`,
			want: []agentlog.Event{{Kind: agentlog.Cmd, Tool: "Bash", Detail: "just test"}},
		},
		{
			name: "a write is a file event counted from its content",
			line: `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":` +
				`{"file_path":"a.go","content":"a\nb\nc\n"}}]}}`,
			want: []agentlog.Event{{Kind: agentlog.File, Tool: "Write", Detail: "a.go", Diff: agentlog.Diff{Added: 3}}},
		},
		{
			name: "a grep renders its pattern",
			line: `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
				`[{"type":"tool_use","name":"Grep","input":{"pattern":"func Parse","output_mode":"content"}}]}}`,
			want: []agentlog.Event{{Kind: agentlog.Tool, Tool: "Grep", Detail: "func Parse"}},
		},
		{
			name: "a tool with no known primary input renders bare",
			line: `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
				`[{"type":"tool_use","name":"ListAgents","input":{}}]}}`,
			want: []agentlog.Event{{Kind: agentlog.Tool, Tool: "ListAgents"}},
		},
		{
			name: "assistant prose is a say event",
			line: `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
				`[{"type":"text","text":"I will start by reading the plan.\n"}]}}`,
			want: []agentlog.Event{{Kind: agentlog.Say, Detail: "I will start by reading the plan."}},
		},
		{
			name: "a call carries its tool_use id",
			line: `{"type":"assistant","message":{"content":` +
				`[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}`,
			want: []agentlog.Event{{Kind: agentlog.Cmd, Tool: "Bash", Detail: "ls", CallID: "toolu_1"}},
		},
		{
			name: "a result keeps every text block and the id of the call it answers",
			line: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1",` +
				`"is_error":true,"content":[{"type":"text","text":"FAIL x"},{"type":"text","text":"exit 1\n"}]}]}}`,
			want: []agentlog.Event{{Kind: agentlog.Fail, Detail: "FAIL x", CallID: "toolu_1",
				Output: "FAIL x\nexit 1", OutputLines: 2}},
		},
		{
			name: "every content block is walked",
			line: `{"type":"assistant","message":{"content":[{"type":"text","text":"Reading both."},` +
				`{"type":"tool_use","name":"Read","input":{"file_path":"a.go"}},` +
				`{"type":"tool_use","name":"Read","input":{"file_path":"b.go"}}]}}`,
			want: []agentlog.Event{
				{Kind: agentlog.Say, Detail: "Reading both."},
				{Kind: agentlog.File, Tool: "Read", Detail: "a.go"},
				{Kind: agentlog.File, Tool: "Read", Detail: "b.go"},
			},
		},
		{
			name: "an edit carries its diff",
			line: `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":` +
				`{"file_path":"a.go","old_string":"a\nb\nc\nd","new_string":"a\nb\nC\nC2\nd"}}]}}`,
			want: []agentlog.Event{{Kind: agentlog.File, Tool: "Edit", Detail: "a.go", Diff: agentlog.Diff{
				Added: 2, Removed: 1, Lines: []agentlog.DiffLine{
					{Op: agentlog.Keep, Text: "a"},
					{Op: agentlog.Keep, Text: "b"},
					{Op: agentlog.Remove, Text: "c"},
					{Op: agentlog.Add, Text: "C"},
					{Op: agentlog.Add, Text: "C2"},
					{Op: agentlog.Keep, Text: "d"},
				},
			}}},
		},
		{
			name: "a long result's output is bounded but counted whole",
			line: `{"type":"user","message":{"content":[{"type":"tool_result","content":"` + output + `"}]}}`,
			want: []agentlog.Event{{
				Kind: agentlog.Pass, Detail: "row", OutputLines: 100,
				Output: strings.Repeat("row\n", 39) + "row\n… output cut short, 60 more lines",
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if diff := cmp.Diff(tt.want, agentlog.ParseLine([]byte(tt.line))); diff != "" {
				t.Errorf("ParseLine(%s) mismatch (-want +got):\n%s", tt.name, diff)
			}
		})
	}

	t.Run("a long diff marks the rows it cuts", func(t *testing.T) {
		t.Parallel()
		line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":` +
			`{"file_path":"a.go","old_string":"","new_string":"` + strings.Repeat("x\\n", 99) + `x"}}]}}`
		got := agentlog.ParseLine([]byte(line))
		if len(got) != 1 {
			t.Fatalf("ParseLine(long edit) = %+v, want one event", got)
		}
		rows := got[0].Diff.Lines
		truncation := agentlog.DiffLine{Op: agentlog.Keep, Text: "… 21 more lines"}
		if len(rows) != 81 || rows[80] != truncation || got[0].Diff.Added != 100 {
			t.Errorf("ParseLine(long edit) diff = %d rows ending %+v, +%d, want 80 rows, a marker for the 21 cut, +100",
				len(rows), rows[len(rows)-1], got[0].Diff.Added)
		}
	})
}

func TestTail_Read(t *testing.T) {
	t.Parallel()

	t.Run("emits the finished Cmd events Parse does", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile("testdata/run27.jsonl")
		if err != nil {
			t.Fatalf("read fixture: %v", err)
		}
		var finished []agentlog.Event
		for _, e := range tailAll(string(data)) {
			if e.Kind == agentlog.Cmd && e.Done {
				finished = append(finished, e)
			}
		}
		want := cmdsOf(parseString(t, string(data)))
		if diff := cmp.Diff(want, finished, cmpopts.IgnoreFields(agentlog.Event{}, "At")); diff != "" {
			t.Errorf("Tail.Read(run27.jsonl) Cmd events mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("drops a skill's launch result across lines", func(t *testing.T) {
		t.Parallel()
		got := tailAll(`{"type":"assistant","message":{"content":[` +
			`{"type":"tool_use","id":"s","name":"Skill","input":{"skill":"tdd"}}]}}` + "\n" +
			`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"s","content":"Launching"}]}}`)
		want := []agentlog.Event{{Kind: agentlog.Skill, Tool: "tdd", CallID: "s"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Tail.Read(skill launch) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a successful git commit finishes as a commit event", func(t *testing.T) {
		t.Parallel()
		for _, tt := range commitCases {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				tailed := tailAll(bashRun(t, tt.command, tt.failed))
				last := tailed[len(tailed)-1]
				if last.Kind != tt.kind || last.Detail != tt.detail || !last.Done {
					t.Errorf("Tail.Read(%q) last event = %+v, want done %v with detail %q", tt.command, last, tt.kind, tt.detail)
				}
			})
		}
	})
}

func FuzzParseLine(f *testing.F) {
	f.Fuzz(func(t *testing.T, line []byte) {
		for _, e := range agentlog.ParseLine(line) {
			if e.OutputLines < 0 {
				t.Errorf("ParseLine(%q) event %+v has negative OutputLines", line, e)
			}
		}
	})
}
