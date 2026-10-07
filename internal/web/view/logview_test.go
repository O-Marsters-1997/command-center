package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

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
	detail := buildLogDetail(testLine, path, false, "sandbox://x", Params{Log: "all"})

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

	ended := buildLogDetail(testLine, writeRunLog(t, testRunLog), false, "sandbox://x", Params{Log: "all"})
	if want := "success · 17m 11s · 129 turns · $8.29"; ended.Result != want {
		t.Errorf("Result = %q, want %q", ended.Result, want)
	}

	firstLine := strings.SplitAfter(testRunLog, "\n")[0]
	alive := buildLogDetail(testLine, writeRunLog(t, firstLine), true, "sandbox://x", Params{Log: "all"})
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
	}{
		{"all", [2]int{2, 2}},
		{"skills", [2]int{1, 0}},
		{"tools", [2]int{2, 1}},
		{"fails", [2]int{1, 0}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()

			detail := buildLogDetail(testLine, path, false, "sandbox://x", Params{Log: tc.mode})
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

	detail := buildLogDetail(testLine, writeRunLog(t, testRunLog), false, "sandbox://x", Params{Log: "all"})

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

func TestBuildLogDetailAnchorSurvivesAFilterThatHidesTheFailure(t *testing.T) {
	t.Parallel()

	path := writeRunLog(t, testRunLog)
	for _, mode := range []string{"skills", "tools"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			detail := buildLogDetail(testLine, path, false, "sandbox://x", Params{Log: mode})
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
	if got := buildLogDetail(testLine, path, true, "x", Params{Log: "all"}).Streaming; !got {
		t.Error("Streaming = false, want true for a live run")
	}
	if got := buildLogDetail(testLine, path, false, "x", Params{Log: "all"}).Streaming; got {
		t.Error("Streaming = true, want false for an ended run")
	}
}

func TestFilterLinksMarkTheActiveModeAndCarryTheSelection(t *testing.T) {
	t.Parallel()

	links := filterLinks(Params{Sel: "sandbox://x", Log: "tools"})
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

			if got := KindShown(tc.mode, tc.kind); got != tc.want {
				t.Errorf("KindShown(%q, %v) = %v, want %v", tc.mode, tc.kind, got, tc.want)
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

func testLine(e agentlog.Event, anchor bool) (string, error) {
	line := "line-" + e.Kind.String() + " " + e.Tool + " " + e.Detail
	if anchor {
		line += ` id="first-fail"`
	}
	return line, nil
}
