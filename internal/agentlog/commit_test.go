package agentlog_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

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

func TestSuccessfulGitCommitIsACommitEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		failed  bool
		kind    agentlog.Kind
		detail  string
	}{
		{"double-quoted message", `git commit -m "fix the thing"`, false, agentlog.Commit, "fix the thing"},
		{"single-quoted message", `git add . && git commit -am 'wip: more'`, false, agentlog.Commit, "wip: more"},
		{"first line of a multi-line message", "git commit -m \"title\n\nbody\"", false, agentlog.Commit, "title"},
		{"heredoc has no message", "git commit -m \"$(cat <<'EOF'\ntitle\nEOF\n)\"", false, agentlog.Commit, ""},
		{"file has no message", `git commit -F msg.txt`, false, agentlog.Commit, ""},
		{"failed commit stays a command", `git commit -m "nope"`, true, agentlog.Cmd, `git commit -m "nope"`},
		{"other git commands stay commands", `git status`, false, agentlog.Cmd, `git status`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log := bashRun(t, tt.command, tt.failed)
			var events []agentlog.Event
			for _, phase := range parseString(t, log).Phases {
				events = append(events, phase.Events...)
			}
			if len(events) != 1 || events[0].Kind != tt.kind || events[0].Detail != tt.detail {
				t.Errorf("Parse events = %+v; want one %v with detail %q", events, tt.kind, tt.detail)
			}

			var tail agentlog.Tail
			var tailed []agentlog.Event
			for line := range strings.SplitSeq(strings.TrimRight(log, "\n"), "\n") {
				tailed = append(tailed, tail.Read([]byte(line))...)
			}
			last := tailed[len(tailed)-1]
			if last.Kind != tt.kind || last.Detail != tt.detail || !last.Done {
				t.Errorf("Tail last event = %+v; want done %v with detail %q", last, tt.kind, tt.detail)
			}
		})
	}
}
