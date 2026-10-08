package view

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

// LineRenderer turns one log line into its markup.
type LineRenderer func(LogLine) (string, error)

// KindShown decides, for one of the four ?log= modes, whether an event's kind renders as a line.
// Phase headers always render regardless of mode; this only gates the lines inside them.
func KindShown(mode string, k agentlog.Kind) bool {
	switch mode {
	case "skills":
		return k == agentlog.Skill
	case "tools":
		return k == agentlog.Tool || k == agentlog.File
	case "fails":
		return k == agentlog.Fail
	default:
		return true
	}
}

type phaseView struct {
	Skill string    `json:"skill"`
	Note  string    `json:"note"`
	At    string    `json:"at"`
	Items []logItem `json:"items"`
}

type logItem struct {
	Kind  string    `json:"kind"`
	Line  string    `json:"line,omitempty"`
	Group callGroup `json:"group"`
}

type callGroup struct {
	Summary  string   `json:"summary"`
	FailNote string   `json:"fail_note"`
	Open     bool     `json:"open"`
	Lines    []string `json:"lines"`
}

type logFilterLink struct {
	Label  string `json:"label"`
	Path   string `json:"path"`
	Active bool   `json:"active"`
}

// LogDetail is the selected row's run log, parsed and filtered, ready for detail.tmpl.
type LogDetail struct {
	Path       string          `json:"path"`
	Streaming  bool            `json:"streaming"`
	Lines      int             `json:"lines"`
	PhaseCount int             `json:"phase_count"`
	Phases     []phaseView     `json:"phases"`
	Outcome    string          `json:"outcome"`
	Filters    []logFilterLink `json:"filters"`
	StreamPath string          `json:"stream_path"`
	Worked     workedLine      `json:"worked"`
	Strip      []phaseSegment  `json:"strip"`
	Readout    readout         `json:"readout"`
	Final      string          `json:"final"`
	Changed    changedFiles    `json:"changed"`
	Jump       jumpLink        `json:"jump"`
}

func buildLogDetail(render LineRenderer, path string, streaming bool, ticketURL string, params Params) LogDetail {
	detail := LogDetail{Path: path, Streaming: streaming, Filters: filterLinks(params)}

	whole, resumeAt := wholeLines(path)
	detail.StreamPath = logStreamPath(ticketURL, resumeAt, params.Log)
	if whole == nil {
		return detail
	}

	run, _ := agentlog.Parse(bytes.NewReader(whole))
	final := takeFinalAnswer(&run)
	stats := measure(run)
	selected := selectedPhase(params.Phase, len(run.Phases))
	mode := NormalizeLogFilter(params.Log)

	detail.Lines = run.Lines
	detail.PhaseCount = len(run.Phases)
	if run.Result != nil {
		detail.Outcome = run.Result.Outcome
	}
	detail.Worked = stats.worked(streaming || selected >= 0 || mode != "all")
	detail.Strip = stats.strip(run.Phases, params, selected)
	detail.Readout = stats.readout(params, selected)
	detail.Phases = renderPhases(render, run.Phases, mode, selected)
	detail.Changed = changedFilesOf(run.Phases)
	detail.Jump = jumpOf(run.Phases, params)
	if final != "" {
		if line, err := render(sayLine(final)); err == nil {
			detail.Final = line
		}
	}
	return detail
}

// wholeLines reads path's complete lines only: an agent flushes mid-line, so a trailing partial
// line is left for the live SSE tail.
func wholeLines(path string) ([]byte, int64) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil, 0
	}
	return data[:end+1], int64(end + 1)
}

func takeFinalAnswer(run *agentlog.Run) string {
	if run.Result == nil || len(run.Phases) == 0 {
		return ""
	}
	last := &run.Phases[len(run.Phases)-1]
	if len(last.Events) == 0 || last.Events[len(last.Events)-1].Kind != agentlog.Say {
		return ""
	}
	final := last.Events[len(last.Events)-1].Detail
	last.Events = last.Events[:len(last.Events)-1]
	return final
}

func selectedPhase(param string, phases int) int {
	i, err := strconv.Atoi(param)
	if err != nil || i < 0 || i >= phases {
		return -1
	}
	return i
}

func renderPhases(render LineRenderer, phases []agentlog.Phase, mode string, selected int) []phaseView {
	firstFail := firstFailureIndex(phases)
	openGroups := mode == "tools" || mode == "fails"
	idx := 0
	var views []phaseView
	for i, phase := range phases {
		b := phaseBuilder{render: render, mode: mode, openGroups: openGroups}
		for _, event := range phase.Events {
			b.add(event, idx == firstFail)
			idx++
		}
		b.flush()
		if selected >= 0 && i != selected {
			continue
		}
		views = append(views, phaseView{Skill: phase.Skill, Note: phase.Note, At: formatOffset(phase.At), Items: b.items})
	}
	return views
}

type phaseBuilder struct {
	render     LineRenderer
	mode       string
	openGroups bool
	items      []logItem
	pending    []callPair
}

func (b *phaseBuilder) add(event agentlog.Event, anchor bool) {
	switch event.Kind {
	case agentlog.Say:
		b.flush()
		if !KindShown(b.mode, event.Kind) {
			return
		}
		if line, err := b.render(sayLine(event.Detail)); err == nil {
			b.items = append(b.items, logItem{Kind: "say", Line: line})
		}
	case agentlog.Pass, agentlog.Fail:
		b.answer(event, anchor)
	default:
		b.pending = append(b.pending, callPair{call: event, called: true})
	}
}

func (b *phaseBuilder) answer(result agentlog.Event, anchor bool) {
	for i := range b.pending {
		p := &b.pending[i]
		if p.called && !p.answered && (result.CallID == "" || p.call.CallID == result.CallID) {
			p.result, p.answered, p.anchor = result, true, anchor
			return
		}
	}
	b.pending = append(b.pending, callPair{result: result, answered: true, anchor: anchor})
}

func (b *phaseBuilder) flush() {
	defer func() { b.pending = nil }()

	var shown []callPair
	hiddenAnchor := false
	for _, p := range b.pending {
		if p.shown(b.mode) {
			shown = append(shown, p)
		} else if p.anchor {
			hiddenAnchor = true
		}
	}
	if len(shown) > 0 {
		group := callGroup{Summary: mixOf(shown).summary(), Open: b.openGroups}
		failed := 0
		for _, p := range shown {
			line, err := b.render(p.line())
			if err != nil {
				continue
			}
			group.Lines = append(group.Lines, line)
			group.Open = group.Open || p.anchor
			if p.failed() {
				failed++
			}
		}
		if failed > 0 {
			group.FailNote = fmt.Sprintf("%d failed", failed)
		}
		b.items = append(b.items, logItem{Kind: "calls", Group: group})
	}
	if hiddenAnchor {
		b.items = append(b.items, logItem{Kind: "anchor"})
	}
}

func firstFailureIndex(phases []agentlog.Phase) int {
	idx := 0
	for _, phase := range phases {
		for _, e := range phase.Events {
			if e.Kind == agentlog.Fail {
				return idx
			}
			idx++
		}
	}
	return -1
}

func filterLinks(params Params) []logFilterLink {
	links := make([]logFilterLink, len(logFilters))
	for i, mode := range logFilters {
		links[i] = logFilterLink{Label: mode, Path: params.withLog(mode).pagePath(), Active: params.Log == mode}
	}
	return links
}

func formatOffset(d time.Duration) string {
	d = d.Round(time.Second)
	m := d / time.Minute
	s := d % time.Minute / time.Second
	return fmt.Sprintf("+%02d:%02d", m, s)
}

func logStreamPath(ticketURL string, from int64, mode string) string {
	path := fmt.Sprintf("/ticket/%s/log?from=%d", url.PathEscape(ticketURL), from)
	if mode != "" && mode != "all" {
		path += "&log=" + url.QueryEscape(mode)
	}
	return path
}
