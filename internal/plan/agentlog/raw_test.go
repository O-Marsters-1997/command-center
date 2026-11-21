package agentlog_test

import (
	"errors"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
)

func TestParseRaw(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 8, 20, 12, 0, 1, 0, time.UTC)
	tests := []struct {
		name string
		log  string
		want []agentlog.RawLine
	}{
		{
			name: "system line summarises its subtype",
			log:  `{"type":"system","subtype":"init"}` + "\n",
			want: []agentlog.RawLine{{N: 1, Type: "system", Summary: "init"}},
		},
		{
			name: "assistant text keeps its first line",
			log: `{"type":"assistant","timestamp":"2026-08-20T12:00:01Z",` +
				`"message":{"content":[{"type":"text","text":"Reading the plan.\nThen more."}]}}` + "\n",
			want: []agentlog.RawLine{{N: 1, At: at, Type: "assistant", Summary: "Reading the plan."}},
		},
		{
			name: "tool use names its tool and primary input",
			log: `{"type":"assistant","message":{"content":[` +
				`{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go test ./..."}}]}}` + "\n",
			want: []agentlog.RawLine{{N: 1, Type: "assistant", Tool: "Bash", Summary: "go test ./..."}},
		},
		{
			name: "a failed tool result carries its call's tool",
			log: `{"type":"assistant","message":{"content":[` +
				`{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go vet"}}]}}` + "\n" +
				`{"type":"user","message":{"content":[` +
				`{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"Exit code 1\nvet: bad"}]}}` + "\n",
			want: []agentlog.RawLine{
				{N: 1, Type: "assistant", Tool: "Bash", Summary: "go vet"},
				{N: 2, Type: "user", Tool: "Bash", Summary: "Exit code 1", Failed: true},
			},
		},
		{
			name: "an error result fails",
			log:  `{"type":"result","subtype":"error_max_turns"}` + "\n",
			want: []agentlog.RawLine{{N: 1, Type: "result", Summary: "error_max_turns", Failed: true}},
		},
		{
			name: "a malformed line is kept and failed, blank lines skipped",
			log:  "{\"type\":\"system\"\n\n" + `{"type":"result","subtype":"success"}` + "\n",
			want: []agentlog.RawLine{
				{N: 1, Type: "malformed", Summary: `{"type":"system"`, Failed: true},
				{N: 3, Type: "result", Summary: "success"},
			},
		},
		{
			name: "an unterminated last line is not read",
			log:  `{"type":"system","subtype":"init"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := agentlog.ParseRaw(strings.NewReader(tt.log))
			if err != nil {
				t.Fatalf("ParseRaw(%q): %v", tt.log, err)
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.IgnoreFields(agentlog.RawLine{}, "JSON")); diff != "" {
				t.Errorf("ParseRaw(%q) mismatch (-want +got):\n%s", tt.log, diff)
			}
		})
	}

	t.Run("JSON is indented, or kept verbatim when malformed", func(t *testing.T) {
		t.Parallel()
		log := `{"type":"system","subtype":"init"}` + "\n" + `{"type":` + "\n"
		got, err := agentlog.ParseRaw(strings.NewReader(log))
		if err != nil {
			t.Fatalf("ParseRaw(%q): %v", log, err)
		}
		want := []string{"{\n  \"type\": \"system\",\n  \"subtype\": \"init\"\n}", `{"type":`}
		var jsons []string
		for _, line := range got {
			jsons = append(jsons, line.JSON)
		}
		if diff := cmp.Diff(want, jsons); diff != "" {
			t.Errorf("ParseRaw(%q) JSON mismatch (-want +got):\n%s", log, diff)
		}
	})

	t.Run("a read error is returned", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		if _, err := agentlog.ParseRaw(iotest.ErrReader(boom)); !errors.Is(err, boom) {
			t.Errorf("ParseRaw(failing reader) error = %v, want %v", err, boom)
		}
	})
}

func FuzzParseRaw(f *testing.F) {
	f.Add(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t","name":"Bash","input":{}}]}}` + "\n")
	f.Add("{\n\n" + `{"type":"user","message":{"content":[{"type":"tool_result","is_error":true,"content":[]}]}}` + "\n")
	f.Fuzz(func(t *testing.T, log string) {
		lines, err := agentlog.ParseRaw(strings.NewReader(log))
		if err != nil {
			t.Fatalf("ParseRaw(%q): %v", log, err)
		}
		if n := strings.Count(log, "\n"); len(lines) > n {
			t.Errorf("ParseRaw(%q) = %d lines, want at most %d", log, len(lines), n)
		}
	})
}
