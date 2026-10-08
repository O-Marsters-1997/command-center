package view

import (
	"os"
	"path/filepath"
	"slices"
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
	if got := groupLineCounts(detail); got != [2]int{1, 1} {
		t.Errorf("group line counts = %v; want one call, paired with its result, per phase", got)
	}
}

func TestBuildLogDetailFoldsTheClosingResultIntoTheWorkedLine(t *testing.T) {
	t.Parallel()

	ended := buildLogDetail(testLine, writeRunLog(t, testRunLog), false, "sandbox://x", Params{Log: "all"})
	if ended.Outcome != "success" {
		t.Errorf("Outcome = %q, want success", ended.Outcome)
	}
	want := workedLine{Label: "Worked for 17m 11s", Stats: "· 129 turns · $8.29"}
	if ended.Worked != want {
		t.Errorf("Worked = %+v, want %+v (closed: the run has ended and nothing is filtered)", ended.Worked, want)
	}

	firstLine := strings.SplitAfter(testRunLog, "\n")[0]
	alive := buildLogDetail(testLine, writeRunLog(t, firstLine), true, "sandbox://x", Params{Log: "all"})
	if alive.Outcome != "" {
		t.Errorf("Outcome = %q, want empty for a run with no result line yet", alive.Outcome)
	}
	if !alive.Worked.Open {
		t.Error("Worked is closed on a live run, which would hide the tail")
	}
}

func TestBuildLogDetailFiltersCallsButKeepsPhaseHeaders(t *testing.T) {
	t.Parallel()

	path := writeRunLog(t, testRunLog)
	for _, tc := range []struct {
		mode      string
		wantLines [2]int
	}{
		{"all", [2]int{1, 1}},
		{"skills", [2]int{0, 0}},
		{"tools", [2]int{1, 1}},
		{"fails", [2]int{1, 0}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()

			detail := buildLogDetail(testLine, path, false, "sandbox://x", Params{Log: tc.mode})
			if len(detail.Phases) != 2 {
				t.Fatalf("phases = %d, want 2", len(detail.Phases))
			}
			if got := groupLineCounts(detail); got != tc.wantLines {
				t.Errorf("mode %q line counts = %v, want %v", tc.mode, got, tc.wantLines)
			}
		})
	}
}

func TestBuildLogDetailAnchorsOnlyTheRunsFirstFailure(t *testing.T) {
	t.Parallel()

	detail := buildLogDetail(testLine, writeRunLog(t, testRunLog), false, "sandbox://x", Params{Log: "all"})

	if got := anchorCount(detail); got != 1 {
		t.Fatalf("%d lines carry the jump anchor, want exactly 1", got)
	}
	failing := detail.Phases[0].Items[0].Group
	if !strings.Contains(failing.Lines[0], `id="first-fail"`) {
		t.Errorf("the run's first Fail does not carry the jump anchor: %s", failing.Lines[0])
	}
	if !failing.Open || failing.FailNote != "1 failed" {
		t.Errorf("failing group = %+v, want it open and noting its failure", failing)
	}
}

func TestBuildLogDetailAnchorSurvivesAFilterThatHidesTheFailure(t *testing.T) {
	t.Parallel()

	path := writeRunLog(t, testRunLog)
	for _, mode := range []string{"skills", "tools"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			detail := buildLogDetail(testLine, path, false, "sandbox://x", Params{Log: mode})
			if anchorCount(detail) != 1 {
				t.Errorf("mode %q renders no #first-fail target, but the jump link still points at it", mode)
			}
		})
	}
}

func TestBuildLogDetailJumpsToTheFirstFailingPhase(t *testing.T) {
	t.Parallel()

	detail := buildLogDetail(testLine, writeRunLog(t, testRunLog), false, "sandbox://x", Params{Sel: "sandbox://x"})
	want := jumpLink{Label: "1 check failed along the way", Path: "/?phase=0&sel=sandbox%3A%2F%2Fx#first-fail"}
	if detail.Jump != want {
		t.Errorf("Jump = %+v, want %+v", detail.Jump, want)
	}
}

var sessionLog = mustReadTestdata("session.jsonl")

func TestBuildLogDetailFoldsEachRunOfCallsIntoOneSummarisedGroup(t *testing.T) {
	t.Parallel()

	detail := buildLogDetail(testLine, writeRunLog(t, sessionLog), false, "x", Params{Log: "all"})

	items := detail.Phases[0].Items
	if len(items) != 2 || items[0].Kind != "say" || items[1].Kind != "calls" {
		t.Fatalf("phase 0 items = %+v, want the prose then one group", items)
	}
	group := items[1].Group
	if group.Summary != "Read 2 files, searched once" || group.Open {
		t.Errorf("group = %+v, want a closed 'Read 2 files, searched once'", group)
	}
	want := []string{"line-file Read a.go 3 lines", "line-file Read b.go 1 line", "line-tool Search x 1 match"}
	if !slices.Equal(group.Lines, want) {
		t.Errorf("group lines = %q, want each call paired with its own result by id: %q", group.Lines, want)
	}
	if len(detail.Phases[1].Items) != 1 {
		t.Errorf("phase 1 items = %+v, want only the Bash group (the skill's own result is dropped)", detail.Phases[1].Items)
	}
}

func TestBuildLogDetailLiftsTheFinalAnswerOutOfTheFold(t *testing.T) {
	t.Parallel()

	detail := buildLogDetail(testLine, writeRunLog(t, sessionLog), false, "x", Params{Log: "all"})
	if detail.Final != "line-say " {
		t.Errorf("Final = %q, want the run's closing prose", detail.Final)
	}
	for _, item := range detail.Phases[1].Items {
		if item.Kind == "say" {
			t.Errorf("the final answer also renders inside the fold: %+v", item)
		}
	}

	live := sessionLog[:strings.LastIndex(strings.TrimSuffix(sessionLog, "\n"), "\n")+1]
	alive := buildLogDetail(testLine, writeRunLog(t, live), true, "x", Params{Log: "all"})
	if alive.Final != "" {
		t.Errorf("Final = %q on a live run, whose last words are not its answer yet", alive.Final)
	}
}

func TestBuildLogDetailShowsOnlyTheSelectedPhase(t *testing.T) {
	t.Parallel()

	path := writeRunLog(t, sessionLog)
	detail := buildLogDetail(testLine, path, false, "x", Params{Sel: "x", Phase: "1"})

	if len(detail.Phases) != 1 || detail.Phases[0].Skill != "tdd" {
		t.Fatalf("Phases = %+v, want only tdd", detail.Phases)
	}
	if !detail.Worked.Open {
		t.Error("Worked is closed with a phase selected, hiding the phase asked for")
	}
	if !detail.Strip[1].Active || detail.Strip[1].Path != "/?sel=x" {
		t.Errorf("selected segment = %+v, want active and toggling the phase off", detail.Strip[1])
	}
	if detail.Strip[0].Path != "/?phase=0&sel=x" {
		t.Errorf("segment 0 Path = %q, want it to select phase 0", detail.Strip[0].Path)
	}
	if detail.Readout.ClearPath != "/?sel=x" {
		t.Errorf("ClearPath = %q, want the whole run", detail.Readout.ClearPath)
	}

	if detail.StreamPath == "" {
		t.Error("StreamPath is empty on the last phase, where the live tail lands")
	}
	if first := buildLogDetail(testLine, path, true, "x", Params{Phase: "0"}); first.StreamPath != "" {
		t.Errorf("StreamPath = %q on an earlier phase, whose view the tail would append to", first.StreamPath)
	}

	for _, param := range []string{"", "9", "-1", "tdd"} {
		if got := buildLogDetail(testLine, path, false, "x", Params{Phase: param}); len(got.Phases) != 2 {
			t.Errorf("phase=%q renders %d phases, want the whole run", param, len(got.Phases))
		}
	}
}

func TestBuildLogDetailWeighsTheStripByEachPhasesDuration(t *testing.T) {
	t.Parallel()

	detail := buildLogDetail(testLine, writeRunLog(t, sessionLog), false, "x", Params{})

	want := []phaseSegment{
		{Label: "start", Duration: "10s", Weight: 10, Path: "/?phase=0"},
		{Label: "tdd", Duration: "30s", Weight: 30, Failed: true, Path: "/?phase=1"},
	}
	if !slices.Equal(detail.Strip, want) {
		t.Errorf("Strip = %+v, want %+v", detail.Strip, want)
	}
}

func TestBuildLogDetailReadsOutTheWholeRun(t *testing.T) {
	t.Parallel()

	detail := buildLogDetail(testLine, writeRunLog(t, sessionLog), false, "x", Params{})

	want := []metric{
		{Label: "Time", Value: "40s"},
		{Label: "Turns", Value: "4"},
		{Label: "Cost", Value: "$2.00"},
		{Label: "Tool calls", Value: "4"},
		{Label: "Failed", Value: "1", Note: "recovered", Failed: true},
	}
	if !slices.Equal(detail.Readout.Metrics, want) {
		t.Errorf("Metrics = %+v, want %+v", detail.Readout.Metrics, want)
	}
	if detail.Readout.Lead != "tdd" {
		t.Errorf("Lead = %q, want the longest phase", detail.Readout.Lead)
	}
	wantInsight := "took 75% of the run and $1.60 of the cost. Select a phase to see where the time went."
	if detail.Readout.Insight != wantInsight {
		t.Errorf("Insight = %q, want %q (the reported cost, apportioned by priced tokens)",
			detail.Readout.Insight, wantInsight)
	}
}

func TestBuildLogDetailReadsOutTheSelectedPhase(t *testing.T) {
	t.Parallel()

	detail := buildLogDetail(testLine, writeRunLog(t, sessionLog), false, "x", Params{Phase: "1"})

	want := []metric{
		{Label: "Time", Value: "30s", Note: "75% of run"},
		{Label: "Turns", Value: "3"},
		{Label: "Cost", Value: "$1.60", Note: "80%"},
		{Label: "Tool calls", Value: "1", Note: "1 running"},
		{Label: "Failed", Value: "1", Failed: true},
	}
	if !slices.Equal(detail.Readout.Metrics, want) {
		t.Errorf("Metrics = %+v, want %+v", detail.Readout.Metrics, want)
	}
	wantInsight := "10s per turn, turns at the run's usual pace; one check failed before it settled."
	if detail.Readout.Lead != "tdd." || detail.Readout.Insight != wantInsight {
		t.Errorf("Readout = %q %q, want %q %q", detail.Readout.Lead, detail.Readout.Insight, "tdd.", wantInsight)
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
		{"all", agentlog.Say, true},
		{"tools", agentlog.Say, false},
		{"fails", agentlog.Say, false},
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

func TestFormatShort(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{41 * time.Second, "41s"},
		{1031 * time.Second, "17m 11s"},
		{3661 * time.Second, "1h 01m"},
	} {
		if got := formatShort(tc.d); got != tc.want {
			t.Errorf("formatShort(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestToolMixSummary(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		mix  toolMix
		want string
	}{
		{"reads and a search", toolMix{reading: 2, searching: 1}, "Read 2 files, searched once"},
		{"one command", toolMix{running: 1}, "Ran 1 command"},
		{"edits and runs", toolMix{editing: 1, running: 3}, "Edited 1 file, ran 3 commands"},
		{"only other tools", toolMix{other: 2}, "Used 2 tools"},
		{"other tools after named ones", toolMix{searching: 2, other: 1}, "Searched twice, used 1 other tool"},
		{"nothing called", toolMix{}, "Tool results"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.mix.summary(); got != tc.want {
				t.Errorf("summary(%v) = %q, want %q", tc.mix, got, tc.want)
			}
		})
	}
}

func TestProseBlocks(t *testing.T) {
	t.Parallel()

	got := proseBlocks("Implemented **it** with `Parse`.\nStill one paragraph.\n\n## Checks\n- `go test`\n- golden")
	want := []ProseBlock{
		{Items: [][]ProseSpan{{
			{Text: "Implemented "}, {Text: "it", Strong: true}, {Text: " with "}, {Text: "Parse", Code: true},
			{Text: ". Still one paragraph."},
		}}},
		{Items: [][]ProseSpan{{{Text: "Checks", Strong: true}}}},
		{List: true, Items: [][]ProseSpan{{{Text: "go test", Code: true}}, {{Text: "golden"}}}},
	}
	if len(got) != len(want) {
		t.Fatalf("proseBlocks = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].List != want[i].List || !slices.EqualFunc(got[i].Items, want[i].Items, slices.Equal) {
			t.Errorf("block %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestSpansLeaveAnUnmatchedDelimiterAsText(t *testing.T) {
	t.Parallel()

	got := spans("a ` b ** c")
	styled := slices.ContainsFunc(got, func(s ProseSpan) bool { return s.Code || s.Strong })
	if text := joinSpans(got); text != "a ` b ** c" || styled {
		t.Errorf("spans = %+v, want plain text throughout", got)
	}
}

func joinSpans(spans []ProseSpan) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

func groupLineCounts(detail LogDetail) [2]int {
	var counts [2]int
	for i, phase := range detail.Phases[:2] {
		for _, item := range phase.Items {
			counts[i] += len(item.Group.Lines)
		}
	}
	return counts
}

func anchorCount(detail LogDetail) int {
	n := 0
	for _, phase := range detail.Phases {
		for _, item := range phase.Items {
			if item.Kind == "anchor" {
				n++
			}
			for _, line := range item.Group.Lines {
				if strings.Contains(line, `id="first-fail"`) {
					n++
				}
			}
		}
	}
	return n
}

func testLine(l LogLine) (string, error) {
	parts := []string{"line-" + l.Kind, l.Verb, l.Arg, l.Result}
	line := strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), " ")
	if l.Kind == "say" {
		line = "line-say "
	}
	if l.Anchor {
		line += ` id="first-fail"`
	}
	return line, nil
}
