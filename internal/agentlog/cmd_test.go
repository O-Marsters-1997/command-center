package agentlog_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

func cmdsOf(run agentlog.Run) []agentlog.Event {
	var cmds []agentlog.Event
	for _, phase := range run.Phases {
		for _, e := range phase.Events {
			if e.Kind == agentlog.Cmd {
				cmds = append(cmds, e)
			}
		}
	}
	return cmds
}

func TestTail_Read(t *testing.T) {
	t.Parallel()
	t.Run("run27 matches the complete parse", func(t *testing.T) {
		t.Parallel()
		data, err := os.ReadFile("testdata/run27.jsonl")
		if err != nil {
			t.Fatalf("ReadFile(run27.jsonl): %v", err)
		}
		findOutput, err := os.ReadFile("testdata/run27.find-output.golden")
		if err != nil {
			t.Fatalf("ReadFile(run27.find-output.golden): %v", err)
		}
		want := agentlog.Run{
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
		}
		parsed := parseString(t, string(data))
		if diff := cmp.Diff(want, parsed, cmpopts.EquateApprox(0, 1e-12)); diff != "" {
			t.Errorf("Parse(run27) mismatch (-want +got):\n%s", diff)
		}
		var tail agentlog.Tail
		var finished []agentlog.Event
		for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
			for _, e := range tail.Read([]byte(line)) {
				if e.Kind == agentlog.Cmd && e.Done {
					finished = append(finished, e)
				}
			}
		}
		commands := cmdsOf(parsed)
		for i := range finished {
			if i >= len(commands) {
				break
			}
			finished[i].At = commands[i].At
		}
		if diff := cmp.Diff(commands, finished); diff != "" {
			t.Errorf("Tail Cmd mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestParseCommandResults(t *testing.T) {
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

	tests := []struct {
		name string
		log  string
		want agentlog.Event
	}{
		{
			name: "stdout and stderr join, skipping an empty part",
			log:  result(stamp, ok, `{"stdout":"out","stderr":"","interrupted":false}`),
			want: agentlog.Event{Done: true, Output: "out", OutputLines: 1, Elapsed: 3500 * time.Millisecond},
		},
		{
			name: "both streams show",
			log:  result(stamp, ok, `{"stdout":"out","stderr":"warn"}`),
			want: agentlog.Event{Done: true, Output: "out\nwarn", OutputLines: 2, Elapsed: 3500 * time.Millisecond},
		},
		{
			name: "interrupted is not a pass",
			log:  result(stamp, ok, `{"stdout":"","stderr":"","interrupted":true}`),
			want: agentlog.Event{
				Done: true, Interrupted: true, Output: "from content", OutputLines: 1, Elapsed: 3500 * time.Millisecond,
			},
		},
		{
			name: "output that merely starts like an exit code is not a failure",
			log:  result(stamp, ok, `{"stdout":"Exit code 3 reached"}`),
			want: agentlog.Event{Done: true, Output: "Exit code 3 reached", OutputLines: 1, Elapsed: 3500 * time.Millisecond},
		},
		{
			name: "a bare string result carries the exit code",
			log:  result(stamp, bad, `"Error: Exit code 2\nboom"`),
			want: agentlog.Event{Done: true, ExitCode: 2, Output: "boom", OutputLines: 1, Elapsed: 3500 * time.Millisecond},
		},
		{
			name: "no tool_use_result falls back to the content text",
			log:  result(stamp, ok, ""),
			want: agentlog.Event{Done: true, Output: "from content", OutputLines: 1, Elapsed: 3500 * time.Millisecond},
		},
		{
			name: "no tool_use_result on a failure reads the exit code from the content",
			log:  result(stamp, bad, ""),
			want: agentlog.Event{
				Done: true, ExitCode: 2, Output: "from content", OutputLines: 1, Elapsed: 3500 * time.Millisecond,
			},
		},
		{
			name: "a result with no timestamp has no duration",
			log:  result("", ok, `{"stdout":"out"}`),
			want: agentlog.Event{Done: true, Output: "out", OutputLines: 1},
		},
		{
			name: "a call never answered is running",
			log:  call,
			want: agentlog.Event{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmds := cmdsOf(parseString(t, tt.log))
			if len(cmds) != 1 {
				t.Fatalf("Parse yields %d Cmd events; want 1 (%+v)", len(cmds), cmds)
			}
			got := cmds[0]
			want := tt.want
			want.Kind, want.Tool, want.Detail, want.CallID = agentlog.Cmd, "Bash", "make", "c"
			got.At = 0
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Cmd mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
