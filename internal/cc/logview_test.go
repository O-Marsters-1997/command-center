package cc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

// testRunLog is one phase of plain tool use, a failure, a Skill boundary, a second phase and a
// closing result -- the shape buildLogDetail is expected to cut and render.
var testRunLog = mustReadTestdata("run.jsonl")

func mustReadTestdata(name string) string {
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		panic(err)
	}
	return string(body)
}

func writeRunLog(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "run.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildLogDetailCutsPhasesAtEachSkill(t *testing.T) {
	t.Parallel()

	path := writeRunLog(t, testRunLog)
	detail := buildLogDetail(path, false, "sandbox://x", viewParams{Log: "all"})

	if detail.Lines != 6 {
		t.Fatalf("Lines = %d, want 6", detail.Lines)
	}
	if len(detail.Phases) != 2 {
		t.Fatalf("Phases = %d, want 2", len(detail.Phases))
	}
	if detail.Phases[0].Skill != "" {
		t.Errorf("phase 0 skill = %q, want empty (before the first Skill call)", detail.Phases[0].Skill)
	}
	if detail.Phases[1].Skill != "tdd" || detail.Phases[1].Note != "writing the test" {
		t.Errorf("phase 1 = %+v, want skill tdd / writing the test", detail.Phases[1])
	}
	if detail.Phases[1].At != "+00:02" {
		t.Errorf("phase 1 At = %q, want +00:02, relative to the first event and monotonic", detail.Phases[1].At)
	}
	if len(detail.Phases[0].Lines) != 2 || len(detail.Phases[1].Lines) != 2 {
		t.Errorf("phase line counts = %d, %d; want 2, 2", len(detail.Phases[0].Lines), len(detail.Phases[1].Lines))
	}
}

func TestBuildLogDetailRendersTheClosingResultLineOnlyOnceEnded(t *testing.T) {
	t.Parallel()

	ended := buildLogDetail(writeRunLog(t, testRunLog), false, "sandbox://x", viewParams{Log: "all"})
	if want := "success · 17m 11s · 129 turns · $8.29"; ended.Result != want {
		t.Errorf("Result = %q, want %q", ended.Result, want)
	}

	firstLine := strings.SplitAfter(testRunLog, "\n")[0]
	alive := buildLogDetail(writeRunLog(t, firstLine), true, "sandbox://x", viewParams{Log: "all"})
	if alive.Result != "" {
		t.Errorf("Result = %q, want empty for a run with no result line yet", alive.Result)
	}
}

func TestBuildLogDetailFiltersEventLinesButKeepsPhaseHeaders(t *testing.T) {
	t.Parallel()

	path := writeRunLog(t, testRunLog)
	for _, tc := range []struct {
		mode      string
		wantLines [2]int // phase 0's own Fail carries the anchor, so a mode that hides it still
		// emits one bare id="first-fail" placeholder line in phase 0's count.
	}{
		{"all", [2]int{2, 2}},
		{"skills", [2]int{1, 0}},
		{"tools", [2]int{2, 1}},
		{"fails", [2]int{1, 0}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()

			detail := buildLogDetail(path, false, "sandbox://x", viewParams{Log: tc.mode})
			if len(detail.Phases) != 2 {
				t.Fatalf("phases = %d, want 2", len(detail.Phases))
			}
			got := [2]int{len(detail.Phases[0].Lines), len(detail.Phases[1].Lines)}
			if got != tc.wantLines {
				t.Errorf("mode %q line counts = %v, want %v", tc.mode, got, tc.wantLines)
			}
		})
	}
}

func TestBuildLogDetailAnchorsOnlyTheRunsFirstFailure(t *testing.T) {
	t.Parallel()

	detail := buildLogDetail(writeRunLog(t, testRunLog), false, "sandbox://x", viewParams{Log: "all"})

	if !strings.Contains(string(detail.Phases[0].Lines[1]), `id="first-fail"`) {
		t.Errorf("the run's first Fail event does not carry the jump anchor: %s", detail.Phases[0].Lines[1])
	}
	for i, phase := range detail.Phases {
		for j, line := range phase.Lines {
			if i == 0 && j == 1 {
				continue
			}
			if strings.Contains(string(line), `id="first-fail"`) {
				t.Errorf("phase %d line %d unexpectedly carries the jump anchor: %s", i, j, line)
			}
		}
	}
}

// TestBuildLogDetailAnchorSurvivesAFilterThatHidesTheFailure covers the "tools" and "skills"
// modes: they filter the Fail event out of the render, but the jump-to-first-failure control
// still renders unconditionally in the header, so #first-fail must still resolve to something.
func TestBuildLogDetailAnchorSurvivesAFilterThatHidesTheFailure(t *testing.T) {
	t.Parallel()

	path := writeRunLog(t, testRunLog)
	for _, mode := range []string{"skills", "tools"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			detail := buildLogDetail(path, false, "sandbox://x", viewParams{Log: mode})
			var found bool
			for _, phase := range detail.Phases {
				for _, line := range phase.Lines {
					if strings.Contains(string(line), `id="first-fail"`) {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("mode %q renders no #first-fail target, but the jump link still points at it", mode)
			}
		})
	}
}

func TestBuildLogDetailCarriesStreaming(t *testing.T) {
	t.Parallel()

	path := writeRunLog(t, testRunLog)
	if got := buildLogDetail(path, true, "x", viewParams{Log: "all"}).Streaming; !got {
		t.Error("Streaming = false, want true for a live run")
	}
	if got := buildLogDetail(path, false, "x", viewParams{Log: "all"}).Streaming; got {
		t.Error("Streaming = true, want false for an ended run")
	}
}

func TestFilterLinksMarkTheActiveModeAndCarryTheSelection(t *testing.T) {
	t.Parallel()

	links := filterLinks(viewParams{Sel: "sandbox://x", Log: "tools"})
	if len(links) != len(logFilters) {
		t.Fatalf("links = %d, want %d", len(links), len(logFilters))
	}
	for _, l := range links {
		if l.Active != (l.Label == "tools") {
			t.Errorf("%s.Active = %v", l.Label, l.Active)
		}
		if !strings.Contains(l.Path, "sel=sandbox") {
			t.Errorf("%s.Path = %q, want the selection carried forward", l.Label, l.Path)
		}
		if l.Label == "all" && strings.Contains(l.Path, "log=all") {
			t.Errorf("all.Path = %q, should not name the default filter", l.Path)
		}
		// A filter link is a plain <a href>, so it must point at the full page ("/"), not the
		// fragment-only /board route -- /board serves the bare <table>, with no page shell, no
		// stylesheet and no htmx script, which is what "zero JavaScript" is asked to still render.
		if strings.HasPrefix(l.Path, "/board") {
			t.Errorf("%s.Path = %q, points at the fragment-only /board route", l.Label, l.Path)
		}
	}
}

func TestKindShownByMode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		mode string
		kind agentlog.Kind
		want bool
	}{
		{"all", agentlog.Tool, true},
		{"all", agentlog.Fail, true},
		{"skills", agentlog.Skill, true},
		{"skills", agentlog.Tool, false},
		{"skills", agentlog.Fail, false},
		{"tools", agentlog.Tool, true},
		{"tools", agentlog.File, true},
		{"tools", agentlog.Fail, false},
		{"fails", agentlog.Fail, true},
		{"fails", agentlog.Pass, false},
	} {
		t.Run(tc.mode+"/"+tc.kind.String(), func(t *testing.T) {
			t.Parallel()

			if got := kindShown(tc.mode, tc.kind); got != tc.want {
				t.Errorf("kindShown(%q, %v) = %v, want %v", tc.mode, tc.kind, got, tc.want)
			}
		})
	}
}

func TestFormatOffset(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{0, "+00:00"},
		{6*time.Minute + 52*time.Second, "+06:52"},
		{90 * time.Second, "+01:30"},
	} {
		if got := formatOffset(tc.d); got != tc.want {
			t.Errorf("formatOffset(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{1031 * time.Second, "17m 11s"},
		{3661 * time.Second, "1h 1m 1s"},
	} {
		if got := formatDuration(tc.d); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

// TestStreamedLineAndServerRenderedLineAreByteIdentical covers the acceptance criterion directly:
// buildLogDetail (the full-render path) and sendLines (the SSE path) both render the same raw
// log line through the same "logline" template, so a line does not change shape at the streaming
// boundary.
func TestStreamedLineAndServerRenderedLineAreByteIdentical(t *testing.T) {
	t.Parallel()

	line := `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
		`[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}` + "\n"
	path := writeRunLog(t, line)

	detail := buildLogDetail(path, false, "sandbox://x", viewParams{Log: "all"})
	if len(detail.Phases) != 1 || len(detail.Phases[0].Lines) != 1 {
		t.Fatalf("detail = %+v, want one phase with one line", detail)
	}
	fromServerRender := string(detail.Phases[0].Lines[0])

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

	path := writeRunLog(t, testRunLog)

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
