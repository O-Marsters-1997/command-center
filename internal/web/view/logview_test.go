package view_test

import (
	"errors"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func readTestdata(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", name, err)
	}
	return string(body)
}

func logDetail(t *testing.T, render view.LineRenderer, src view.LogSource, mode view.LogFilter) view.LogDetail {
	t.Helper()
	detail, err := view.BuildLogDetail(render, src, view.Params{Log: mode})
	if err != nil {
		t.Fatalf("BuildLogDetail(%+v): %v", src, err)
	}
	return detail
}

func testLine(l view.LogLine) (template.HTML, error) {
	parts := []string{"line-" + l.Kind, l.Verb, l.Arg, l.Result}
	line := strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), " ")
	if l.Kind == "say" {
		line = "line-say "
	}
	if l.Anchor {
		line += ` id="first-fail"`
	}
	return template.HTML(line), nil
}

func groupLineCounts(detail view.LogDetail) [2]int {
	var counts [2]int
	for i, phase := range detail.Phases[:2] {
		for _, item := range phase.Items {
			counts[i] += len(item.Group.Lines)
		}
	}
	return counts
}

func anchorCount(detail view.LogDetail) int {
	n := 0
	for _, phase := range detail.Phases {
		for _, item := range phase.Items {
			if item.Kind == "anchor" {
				n++
			}
			for _, line := range item.Group.Lines {
				if strings.Contains(string(line), `id="first-fail"`) {
					n++
				}
			}
		}
	}
	return n
}

func toolCalls(tools ...string) string {
	var b strings.Builder
	for i, tool := range tools {
		fmt.Fprintf(&b, `{"type":"assistant","timestamp":"2026-01-01T00:00:%02dZ","message":{"content":`+
			`[{"type":"tool_use","id":"t%d","name":%q,"input":{"file_path":"f.go","pattern":"x","command":"ls"}}]}}`+"\n",
			i, i, tool)
	}
	return b.String()
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

var allModes = []view.LogFilter{view.LogAll, view.LogSkills, view.LogTools, view.LogFails}

func TestKindShown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode view.LogFilter
		kind agentlog.Kind
		want bool
	}{
		{mode: view.LogAll, kind: agentlog.Tool, want: true},
		{mode: view.LogAll, kind: agentlog.Fail, want: true},
		{mode: view.LogSkills, kind: agentlog.Skill, want: true},
		{mode: view.LogSkills, kind: agentlog.Tool},
		{mode: view.LogSkills, kind: agentlog.Fail},
		{mode: view.LogTools, kind: agentlog.Tool, want: true},
		{mode: view.LogTools, kind: agentlog.File, want: true},
		{mode: view.LogTools, kind: agentlog.Fail},
		{mode: view.LogFails, kind: agentlog.Fail, want: true},
		{mode: view.LogFails, kind: agentlog.Pass},
		{mode: view.LogAll, kind: agentlog.Commit, want: true},
		{mode: view.LogTools, kind: agentlog.Commit, want: true},
		{mode: view.LogSkills, kind: agentlog.Commit},
		{mode: view.LogFails, kind: agentlog.Commit},
		{mode: view.LogAll, kind: agentlog.Say, want: true},
		{mode: view.LogTools, kind: agentlog.Say},
		{mode: view.LogFails, kind: agentlog.Say},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s %s", tt.mode, tt.kind), func(t *testing.T) {
			t.Parallel()
			if got := view.KindShown(tt.mode, tt.kind); got != tt.want {
				t.Errorf("KindShown(%q, %v) = %v, want %v", tt.mode, tt.kind, got, tt.want)
			}
		})
	}
}

func TestEventShown(t *testing.T) {
	t.Parallel()

	passed := agentlog.Event{Kind: agentlog.Cmd, Done: true}
	failed := agentlog.Event{Kind: agentlog.Cmd, Done: true, ExitCode: 1}
	running := agentlog.Event{Kind: agentlog.Cmd}
	tests := []struct {
		name  string
		mode  view.LogFilter
		event agentlog.Event
		want  bool
	}{
		{name: "all passed", mode: view.LogAll, event: passed, want: true},
		{name: "tools passed", mode: view.LogTools, event: passed, want: true},
		{name: "tools running", mode: view.LogTools, event: running, want: true},
		{name: "skills failed", mode: view.LogSkills, event: failed},
		{name: "fails passed", mode: view.LogFails, event: passed},
		{name: "fails running", mode: view.LogFails, event: running},
		{name: "fails failed", mode: view.LogFails, event: failed, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := view.EventShown(tt.mode, tt.event); got != tt.want {
				t.Errorf("EventShown(%q, %+v) = %v, want %v", tt.mode, tt.event, got, tt.want)
			}
		})
	}
}

func TestBuildLogDetail(t *testing.T) {
	t.Parallel()

	runLog := readTestdata(t, "run.jsonl")
	sessionLog := readTestdata(t, "session.jsonl")

	t.Run("refuses a line that fails to render", func(t *testing.T) {
		t.Parallel()
		errRender := errors.New("render failed")
		failing := func(view.LogLine) (template.HTML, error) { return "", errRender }
		src := view.LogSource{Path: writeRunLog(t, runLog), TicketURL: "sandbox://x"}

		if _, err := view.BuildLogDetail(failing, src, view.Params{Log: view.LogAll}); !errors.Is(err, errRender) {
			t.Errorf("BuildLogDetail(failing render) error = %v, want %v", err, errRender)
		}
	})

	t.Run("cuts phases at each skill", func(t *testing.T) {
		t.Parallel()
		detail := logDetail(t, testLine, view.LogSource{Path: writeRunLog(t, runLog), TicketURL: "sandbox://x"}, view.LogAll)

		type phase struct{ Skill, Note, At string }
		var got []phase
		for _, p := range detail.Phases {
			got = append(got, phase{Skill: p.Skill, Note: p.Note, At: p.At})
		}
		want := []phase{{At: "+00:00"}, {Skill: "tdd", Note: "writing the test", At: "+00:02"}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("BuildLogDetail(run) phases mismatch (-want +got):\n%s", diff)
		}
		if detail.Lines != 6 {
			t.Errorf("BuildLogDetail(run).Lines = %d, want 6", detail.Lines)
		}
		if got, want := groupLineCounts(detail), [2]int{1, 1}; got != want {
			t.Errorf("BuildLogDetail(run) group line counts = %v, want %v", got, want)
		}
	})

	t.Run("filters calls but keeps phase headers", func(t *testing.T) {
		t.Parallel()
		path := writeRunLog(t, runLog)
		tests := []struct {
			mode      view.LogFilter
			wantLines [2]int
		}{
			{mode: view.LogAll, wantLines: [2]int{1, 1}},
			{mode: view.LogSkills, wantLines: [2]int{0, 0}},
			{mode: view.LogTools, wantLines: [2]int{1, 1}},
			{mode: view.LogFails, wantLines: [2]int{1, 0}},
		}
		for _, tt := range tests {
			t.Run(string(tt.mode), func(t *testing.T) {
				t.Parallel()
				detail := logDetail(t, testLine, view.LogSource{Path: path, TicketURL: "sandbox://x"}, tt.mode)
				if len(detail.Phases) != 2 {
					t.Fatalf("BuildLogDetail(%s) phases = %d, want 2", tt.mode, len(detail.Phases))
				}
				if got := groupLineCounts(detail); got != tt.wantLines {
					t.Errorf("BuildLogDetail(%s) group line counts = %v, want %v", tt.mode, got, tt.wantLines)
				}
			})
		}
	})

	t.Run("places store events among the agent's lines by time", func(t *testing.T) {
		t.Parallel()
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		records := []store.Event{
			{ID: 4, At: start.Add(-time.Minute), Kind: "pr_merged", Detail: "PR #11 merged"},
			{ID: 5, At: start.Add(time.Minute), Kind: "launched", Detail: "internal audit row"},
			{ID: 7, At: start.Add(time.Hour), Kind: "pr_merged", Detail: "PR #12 merged"},
		}
		src := view.LogSource{Path: writeRunLog(t, runLog), TicketURL: "sandbox://x", Records: records}

		detail := logDetail(t, testLine, src, view.LogAll)

		first, last := detail.Phases[0].Items[0], detail.Phases[len(detail.Phases)-1].Items
		if first.Kind != "record" || first.Line != "line-record merged PR #11 merged" {
			t.Errorf("BuildLogDetail() first item = %+v, want the early record", first)
		}
		if got := last[len(last)-1]; got.Kind != "record" || got.Line != "line-record merged PR #12 merged" {
			t.Errorf("BuildLogDetail() last item = %+v, want the late record", got)
		}
		if !strings.HasSuffix(detail.StreamPath, "&records=7") {
			t.Errorf("BuildLogDetail().StreamPath = %q, want it to resume after record 7", detail.StreamPath)
		}

		for _, mode := range allModes[1:] {
			t.Run(string(mode), func(t *testing.T) {
				t.Parallel()
				for _, phase := range logDetail(t, testLine, src, mode).Phases {
					for _, item := range phase.Items {
						if item.Kind == "record" {
							t.Errorf("BuildLogDetail(%s) rendered record %q, want records under all only", mode, item.Line)
						}
					}
				}
			})
		}
	})

	t.Run("renders store events when the log is gone", func(t *testing.T) {
		t.Parallel()
		src := view.LogSource{
			Path: filepath.Join(t.TempDir(), "pruned.jsonl"), TicketURL: "sandbox://x",
			Records: []store.Event{{ID: 3, At: testNow, Kind: "pr_merged", Detail: "PR #12 merged"}},
		}

		detail := logDetail(t, testLine, src, view.LogAll)

		if len(detail.Phases) != 1 || len(detail.Phases[0].Items) != 1 {
			t.Fatalf("BuildLogDetail(pruned) phases = %+v, want one phase holding one line", detail.Phases)
		}
		if got, want := detail.Phases[0].Items[0].Line, template.HTML("line-record merged PR #12 merged"); got != want {
			t.Errorf("BuildLogDetail(pruned) line = %q, want %q", got, want)
		}
		if detail.PhaseCount != 0 {
			t.Errorf("BuildLogDetail(pruned).PhaseCount = %d, want 0", detail.PhaseCount)
		}
	})

	t.Run("worked line", func(t *testing.T) {
		t.Parallel()
		ran := func(ms int) string {
			return toolCalls("Read") + fmt.Sprintf(`{"type":"result","subtype":"success","duration_ms":%d,"num_turns":1}`, ms) + "\n"
		}
		tests := []struct {
			name      string
			log       string
			streaming bool
			wantLabel string
			wantStats string
		}{
			{name: "a recorded run", log: runLog, wantLabel: "Worked for 17m 11s", wantStats: "· 129 turns"},
			{name: "seconds", log: ran(41000), wantLabel: "Worked for 41s", wantStats: "· 1 turn"},
			{name: "hours", log: ran(3661000), wantLabel: "Worked for 1h 01m", wantStats: "· 1 turn"},
			{
				name: "a live run has no result line yet", log: strings.SplitAfter(runLog, "\n")[0], streaming: true,
				wantLabel: "Worked for 0s",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				src := view.LogSource{Path: writeRunLog(t, tt.log), Streaming: tt.streaming, TicketURL: "sandbox://x"}
				worked := logDetail(t, testLine, src, view.LogAll).Worked
				if worked.Label != tt.wantLabel || worked.Stats != tt.wantStats {
					t.Errorf("BuildLogDetail(%s).Worked = {%q %q}, want {%q %q}",
						tt.name, worked.Label, worked.Stats, tt.wantLabel, tt.wantStats)
				}
			})
		}
	})

	t.Run("anchors only the run's first failure", func(t *testing.T) {
		t.Parallel()
		detail := logDetail(t, testLine, view.LogSource{Path: writeRunLog(t, runLog), TicketURL: "sandbox://x"}, view.LogAll)

		if got := anchorCount(detail); got != 1 {
			t.Fatalf("BuildLogDetail(run) anchors = %d, want 1", got)
		}
		failing := detail.Phases[0].Items[0].Group
		if !strings.Contains(string(failing.Lines[0]), `id="first-fail"`) || !failing.Open || failing.FailNote == "" {
			t.Errorf("BuildLogDetail(run) failing group = %+v, want it anchored, open and noting its failure", failing)
		}
	})

	t.Run("the anchor survives a filter that hides the failure", func(t *testing.T) {
		t.Parallel()
		path := writeRunLog(t, runLog)
		for _, mode := range []view.LogFilter{view.LogSkills, view.LogTools} {
			t.Run(string(mode), func(t *testing.T) {
				t.Parallel()
				detail := logDetail(t, testLine, view.LogSource{Path: path, TicketURL: "sandbox://x"}, mode)
				if got := anchorCount(detail); got != 1 {
					t.Errorf("BuildLogDetail(%s) anchors = %d, want 1", mode, got)
				}
			})
		}
	})

	t.Run("folds each run of calls into one summarised group", func(t *testing.T) {
		t.Parallel()
		detail := logDetail(t, testLine, view.LogSource{Path: writeRunLog(t, sessionLog), TicketURL: "x"}, view.LogAll)

		items := detail.Phases[0].Items
		if len(items) != 2 || items[0].Kind != "say" || items[1].Kind != "calls" {
			t.Fatalf("BuildLogDetail(session) phase 0 items = %+v, want the prose then one group", items)
		}
		group := items[1].Group
		if group.Open {
			t.Errorf("BuildLogDetail(session) group.Open = true, want a closed group")
		}
		want := []template.HTML{"line-file Read a.go 3 lines", "line-file Read b.go 1 line", "line-tool Search x 1 match"}
		if diff := cmp.Diff(want, group.Lines); diff != "" {
			t.Errorf("BuildLogDetail(session) group lines mismatch (-want +got):\n%s", diff)
		}
		if len(detail.Phases[1].Items) != 1 {
			t.Errorf("BuildLogDetail(session) phase 1 items = %+v, want only the Bash group", detail.Phases[1].Items)
		}
	})

	t.Run("group summary", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name  string
			tools []string
			want  string
		}{
			{name: "reads and a search", tools: []string{"Read", "Read", "Grep"}, want: "Read 2 files, searched once"},
			{name: "one command", tools: []string{"Bash"}, want: "Ran 1 command"},
			{name: "edits and runs", tools: []string{"Edit", "Bash", "Bash", "Bash"}, want: "Edited 1 file, ran 3 commands"},
			{name: "only other tools", tools: []string{"WebFetch", "WebFetch"}, want: "Used 2 tools"},
			{name: "other tools after named ones", tools: []string{"Glob", "Grep", "WebFetch"}, want: "Searched twice, used 1 other tool"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				src := view.LogSource{Path: writeRunLog(t, toolCalls(tt.tools...)), TicketURL: "x"}
				items := logDetail(t, testLine, src, view.LogAll).Phases[0].Items
				if len(items) != 1 || items[0].Group.Summary != tt.want {
					t.Errorf("BuildLogDetail(%v) items = %+v, want one group summarised %q", tt.tools, items, tt.want)
				}
			})
		}
	})

	t.Run("lifts the final answer out of the fold", func(t *testing.T) {
		t.Parallel()
		detail := logDetail(t, testLine, view.LogSource{Path: writeRunLog(t, sessionLog), TicketURL: "x"}, view.LogAll)
		if detail.Final != "line-say " {
			t.Errorf("BuildLogDetail(session).Final = %q, want the closing prose", detail.Final)
		}
		for _, item := range detail.Phases[1].Items {
			if item.Kind == "say" {
				t.Errorf("BuildLogDetail(session) also folded the final answer: %+v", item)
			}
		}

		live := sessionLog[:strings.LastIndex(strings.TrimSuffix(sessionLog, "\n"), "\n")+1]
		alive := logDetail(t, testLine, view.LogSource{Path: writeRunLog(t, live), Streaming: true, TicketURL: "x"}, view.LogAll)
		if alive.Final != "" {
			t.Errorf("BuildLogDetail(live session).Final = %q, want none", alive.Final)
		}
	})

	t.Run("carries streaming", func(t *testing.T) {
		t.Parallel()
		path := writeRunLog(t, runLog)
		for _, streaming := range []bool{true, false} {
			src := view.LogSource{Path: path, Streaming: streaming, TicketURL: "x"}
			if got := logDetail(t, testLine, src, view.LogAll).Streaming; got != streaming {
				t.Errorf("BuildLogDetail(streaming %v).Streaming = %v, want %v", streaming, got, streaming)
			}
		}
	})

	t.Run("a successful commit is its own line", func(t *testing.T) {
		t.Parallel()
		const log = `{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"content":` +
			`[{"type":"tool_use","id":"c","name":"Bash","input":{"command":"git commit -m \"add it\""}}]}}` + "\n" +
			`{"type":"user","timestamp":"2026-01-01T00:00:01Z","message":{"content":` +
			`[{"type":"tool_result","tool_use_id":"c","content":"ok"}]}}` + "\n"

		detail := logDetail(t, testLine, view.LogSource{Path: writeRunLog(t, log), TicketURL: "sandbox://x"}, view.LogAll)

		if len(detail.Phases) != 1 || len(detail.Phases[0].Items) != 1 {
			t.Fatalf("BuildLogDetail(commit) phases = %+v, want one phase with one item", detail.Phases)
		}
		if item := detail.Phases[0].Items[0]; item.Kind != "commit" || item.Line != "line-commit commit add it ✓ 1s" {
			t.Errorf("BuildLogDetail(commit) item = %+v, want a commit line carrying the message", item)
		}
	})

	t.Run("edit and write counts", func(t *testing.T) {
		t.Parallel()
		body, err := os.ReadFile(filepath.Join("..", "..", "agentlog", "testdata", "edits.jsonl"))
		if err != nil {
			t.Fatalf("os.ReadFile(edits.jsonl): %v", err)
		}
		counts := func(l view.LogLine) (template.HTML, error) {
			return template.HTML(fmt.Sprintf("+%d -%d", l.Added, l.Removed)), nil
		}
		tests := []struct {
			name string
			log  string
			want []string
		}{
			{name: "from the structured result", log: string(body), want: []string{"+3 -2", "+3 -0", "+3 -1"}},
			{name: "from the inputs when there is no result", log: stripToolUseResults(string(body)), want: []string{"+1 -1", "+3 -0", "+5 -0"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				detail := logDetail(t, counts, view.LogSource{Path: writeRunLog(t, tt.log), TicketURL: "sandbox://x"}, view.LogAll)
				var got []string
				for _, item := range detail.Phases[0].Items {
					for _, line := range item.Group.Lines {
						got = append(got, string(line))
					}
				}
				if diff := cmp.Diff(tt.want, got); diff != "" {
					t.Errorf("BuildLogDetail(%s) counts mismatch (-want +got):\n%s", tt.name, diff)
				}
			})
		}
	})
}
