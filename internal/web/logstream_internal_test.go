package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
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

// TestStreamedLineAndServerRenderedLineAreByteIdentical covers the acceptance criterion directly:
// the full-render path and sendLines (the SSE path) both render a raw log line through the same
// "logline" template, so a line does not change shape at the streaming boundary.
func TestStreamedLineAndServerRenderedLineAreByteIdentical(t *testing.T) {
	t.Parallel()

	line := `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
		`[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}` + "\n"
	path := writeRunLog(t, line)

	event, ok := agentlog.ParseLine([]byte(strings.TrimRight(line, "\n")))
	if !ok {
		t.Fatal("fixture line did not parse")
	}
	fromServerRender, err := renderLogLine(event, false)
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

// TestSendLinesAppliesTheCurrentFilter covers the live half of a filtered panel: a line arriving
// over SSE while ?log=fails is selected should not sneak an unfiltered Tool event into a view
// that otherwise only ever shows Fail events.
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
