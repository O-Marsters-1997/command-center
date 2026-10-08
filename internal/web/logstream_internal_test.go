package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func writeRunLog(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "run.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustReadTestdata(name string) string {
	body, err := os.ReadFile(filepath.Join("view", "testdata", name))
	if err != nil {
		panic(err)
	}
	return string(body)
}

func TestStreamedLineAndServerRenderedLineAreByteIdentical(t *testing.T) {
	t.Parallel()

	line := `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
		`[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}` + "\n"
	path := writeRunLog(t, line)

	events := agentlog.ParseLine([]byte(strings.TrimRight(line, "\n")))
	if len(events) != 1 {
		t.Fatalf("fixture line parsed to %d events, want 1", len(events))
	}
	fromServerRender, err := renderLogLine(view.LineOf(events[0]))
	if err != nil {
		t.Fatal(err)
	}

	var buf strings.Builder
	offset := int64(0)
	if sent := sendLines(&buf, path, &offset, "all"); sent != 1 {
		t.Fatalf("sendLines sent %d events, want 1", sent)
	}
	fromStream := extractSSEData(t, buf.String())

	if fromStream != fromServerRender {
		t.Errorf("streamed line = %q\nserver-rendered line = %q\nwant byte-identical", fromStream, fromServerRender)
	}
}
func extractSSEData(t *testing.T, frame string) string {
	t.Helper()

	_, data, ok := strings.Cut(frame, "data: ")
	if !ok {
		t.Fatalf("no data field in SSE frame: %q", frame)
	}
	return strings.TrimSuffix(data, "\n\n")
}

func TestSendLinesAppliesTheCurrentFilter(t *testing.T) {
	t.Parallel()

	path := writeRunLog(t, mustReadTestdata("run.jsonl"))

	var buf strings.Builder
	offset := int64(0)
	sent := sendLines(&buf, path, &offset, "fails")

	if sent != 1 {
		t.Fatalf("sendLines(mode=fails) sent %d events, want 1 (just the run's one Fail)", sent)
	}
	if got := buf.String(); !strings.Contains(got, "line-fail") || strings.Contains(got, "line-tool") {
		t.Errorf("sendLines(mode=fails) sent a non-Fail line: %q", got)
	}
}

func TestSendLinesCarriesAMultiLineOutputAsOneDataLinePerLine(t *testing.T) {
	t.Parallel()

	line := `{"type":"user","message":{"content":[{"type":"tool_result","is_error":true,` +
		`"content":"--- FAIL: TestX\nwant y"}]}}` + "\n"
	path := writeRunLog(t, line)

	var buf strings.Builder
	offset := int64(0)
	if sent := sendLines(&buf, path, &offset, "all"); sent != 1 {
		t.Fatalf("sendLines sent %d events, want 1", sent)
	}
	frame := buf.String()
	if !strings.Contains(frame, "<pre class=\"call-out\">--- FAIL: TestX\ndata: want y</pre>") {
		t.Errorf("the output's second line is not its own data line, so EventSource would drop it: %q", frame)
	}
	if !strings.HasSuffix(frame, "</div>\n\n") || strings.Count(frame, "\n\n") != 1 {
		t.Errorf("frame = %q, want exactly one event", frame)
	}
}
