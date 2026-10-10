package agentlog_test

import (
	"os"
	"strings"
	"testing"
	"time"

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

func sameCmd(got, want agentlog.Event) bool {
	return sameEvent(got, want) && got.Done == want.Done && got.ExitCode == want.ExitCode &&
		got.Interrupted == want.Interrupted && got.Elapsed == want.Elapsed
}

func TestParseRun27Commands(t *testing.T) {
	t.Parallel()

	f, err := os.Open("testdata/run27.jsonl")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	run, err := agentlog.Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	cmds := cmdsOf(run)
	if len(cmds) != 2 {
		t.Fatalf("Parse yields %d Cmd events; want 2 (%+v)", len(cmds), cmds)
	}
	passed, failed := cmds[0], cmds[1]
	if !passed.Done || passed.ExitCode != 0 || passed.Elapsed <= 0 ||
		!strings.HasPrefix(passed.Output, "internal/cc/app_test.go\ninternal/cc/app.go\n") {
		t.Errorf("passing Cmd = %+v; want exit 0, a duration and stdout as output", passed)
	}
	if !failed.Done || failed.ExitCode != 1 || failed.Elapsed <= 0 ||
		failed.Output != "go is /opt/homebrew/bin/go\nrtk is /opt/homebrew/bin/rtk" {
		t.Errorf("failing Cmd = %+v; want exit 1, a duration and the output without its Exit code prefix", failed)
	}
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
			want: agentlog.Event{Done: true, Interrupted: true, Output: "from content", OutputLines: 1, Elapsed: 3500 * time.Millisecond},
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
			want: agentlog.Event{Done: true, ExitCode: 2, Output: "from content", OutputLines: 1, Elapsed: 3500 * time.Millisecond},
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
			if !sameCmd(got, want) {
				t.Errorf("Cmd = %+v; want %+v", got, want)
			}
		})
	}
}

func TestTailEmitsTheCmdEventsParseDoes(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/run27.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
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

	want := cmdsOf(parseString(t, string(data)))
	if len(finished) != len(want) {
		t.Fatalf("Tail finished %d Cmd events; Parse yields %d", len(finished), len(want))
	}
	for i := range want {
		got := finished[i]
		got.At = want[i].At
		if !sameCmd(got, want[i]) {
			t.Errorf("Tail Cmd %d = %+v; Parse = %+v", i, got, want[i])
		}
	}
}
