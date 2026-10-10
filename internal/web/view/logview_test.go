package view

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/store"
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
	detail := buildLogDetail(testLine, path, false, "sandbox://x", nil, Params{Log: "all"})

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

func TestBuildLogDetailPlacesStoreEventsAmongTheAgentsLinesByTime(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	records := []store.Event{
		{ID: 4, At: start.Add(-time.Minute), Kind: "launched", Detail: "before the log"},
		{ID: 7, At: start.Add(time.Hour), Kind: "pr_merged", Detail: "PR #12 merged"},
	}
	path := writeRunLog(t, testRunLog)

	detail := buildLogDetail(testLine, path, false, "sandbox://x", records, Params{Log: "all"})

	first, last := detail.Phases[0], detail.Phases[len(detail.Phases)-1]
	if got := first.Items[0]; got.Kind != "record" || got.Line != "line-record launched before the log" {
		t.Errorf("first item = %+v, want the early record ahead of the agent's lines", got)
	}
	if got := last.Items[len(last.Items)-1]; got.Kind != "record" || got.Line != "line-record merged PR #12 merged" {
		t.Errorf("last item = %+v, want the merge line after the agent's lines", got)
	}
	if !strings.HasSuffix(detail.StreamPath, "&records=7") {
		t.Errorf("StreamPath = %q, want it to resume after record 7", detail.StreamPath)
	}

	for _, mode := range []string{"skills", "tools", "fails"} {
		hidden := buildLogDetail(testLine, path, false, "sandbox://x", records, Params{Log: mode})
		for _, phase := range hidden.Phases {
			for _, item := range phase.Items {
				if item.Kind == "record" {
					t.Errorf("mode %q rendered a store line; they show under all only", mode)
				}
			}
		}
	}
}

func TestBuildLogDetailRendersStoreEventsWhenTheLogIsGone(t *testing.T) {
	t.Parallel()

	records := []store.Event{{ID: 3, At: time.Now(), Kind: "pr_merged", Detail: "PR #12 merged"}}
	gone := filepath.Join(t.TempDir(), "pruned.jsonl")

	detail := buildLogDetail(testLine, gone, false, "sandbox://x", records, Params{Log: "all"})

	if len(detail.Phases) != 1 || len(detail.Phases[0].Items) != 1 {
		t.Fatalf("Phases = %+v, want one phase holding the one store line", detail.Phases)
	}
	if got, want := detail.Phases[0].Items[0].Line, "line-record merged PR #12 merged"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
	if detail.PhaseCount != 0 {
		t.Errorf("PhaseCount = %d, want 0: no agent phase ran in what is left", detail.PhaseCount)
	}
}

func TestBuildLogDetailFoldsTheClosingResultIntoTheWorkedLine(t *testing.T) {
	t.Parallel()

	ended := buildLogDetail(testLine, writeRunLog(t, testRunLog), false, "sandbox://x", nil, Params{Log: "all"})
	want := workedLine{Label: "Worked for 17m 11s", Stats: "· 129 turns · $8.29"}
	if ended.Worked != want {
		t.Errorf("Worked = %+v, want %+v (closed: the run has ended and nothing is filtered)", ended.Worked, want)
	}

	firstLine := strings.SplitAfter(testRunLog, "\n")[0]
	alive := buildLogDetail(testLine, writeRunLog(t, firstLine), true, "sandbox://x", nil, Params{Log: "all"})
	if alive.Worked.Stats != "" {
		t.Errorf("Worked.Stats = %q, want none for a run with no result line yet", alive.Worked.Stats)
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

			detail := buildLogDetail(testLine, path, false, "sandbox://x", nil, Params{Log: tc.mode})
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

	detail := buildLogDetail(testLine, writeRunLog(t, testRunLog), false, "sandbox://x", nil, Params{Log: "all"})

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

			detail := buildLogDetail(testLine, path, false, "sandbox://x", nil, Params{Log: mode})
			if anchorCount(detail) != 1 {
				t.Errorf("mode %q renders no #first-fail target, but the jump link still points at it", mode)
			}
		})
	}
}

var sessionLog = mustReadTestdata("session.jsonl")

func TestBuildLogDetailFoldsEachRunOfCallsIntoOneSummarisedGroup(t *testing.T) {
	t.Parallel()

	detail := buildLogDetail(testLine, writeRunLog(t, sessionLog), false, "x", nil, Params{Log: "all"})

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

	detail := buildLogDetail(testLine, writeRunLog(t, sessionLog), false, "x", nil, Params{Log: "all"})
	if detail.Final != "line-say " {
		t.Errorf("Final = %q, want the run's closing prose", detail.Final)
	}
	for _, item := range detail.Phases[1].Items {
		if item.Kind == "say" {
			t.Errorf("the final answer also renders inside the fold: %+v", item)
		}
	}

	live := sessionLog[:strings.LastIndex(strings.TrimSuffix(sessionLog, "\n"), "\n")+1]
	alive := buildLogDetail(testLine, writeRunLog(t, live), true, "x", nil, Params{Log: "all"})
	if alive.Final != "" {
		t.Errorf("Final = %q on a live run, whose last words are not its answer yet", alive.Final)
	}
}

func TestBuildLogDetailCarriesStreaming(t *testing.T) {
	t.Parallel()

	path := writeRunLog(t, testRunLog)
	if got := buildLogDetail(testLine, path, true, "x", nil, Params{Log: "all"}).Streaming; !got {
		t.Error("Streaming = false, want true for a live run")
	}
	if got := buildLogDetail(testLine, path, false, "x", nil, Params{Log: "all"}).Streaming; got {
		t.Error("Streaming = true, want false for an ended run")
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

func TestBuildLogDetailShowsEditAndWriteCounts(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(filepath.Join("..", "..", "agentlog", "testdata", "edits.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	log := string(body)
	withoutResults := stripToolUseResults(log)
	for _, tc := range []struct {
		name string
		log  string
		want []string
	}{
		{"from the structured result", log, []string{"+3 -2", "+3 -0", "+3 -1"}},
		{"from the inputs when there is no result", withoutResults, []string{"+1 -1", "+3 -0", "+5 -0"}},
	} {
		render := func(l LogLine) (string, error) { return fmt.Sprintf("+%d -%d", l.Added, l.Removed), nil }
		detail := buildLogDetail(render, writeRunLog(t, tc.log), false, "sandbox://x", nil, Params{Log: "all"})

		var got []string
		for _, item := range detail.Phases[0].Items {
			got = append(got, item.Group.Lines...)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: counts = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func stripToolUseResults(log string) string {
	var kept []string
	for _, line := range strings.SplitAfter(log, "\n") {
		if before, _, found := strings.Cut(line, `,"tool_use_result"`); found {
			line = before + "}\n"
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
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
