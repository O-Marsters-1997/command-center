package cc

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"os"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

//go:embed logline.tmpl
var loglineSource string

// Registered into page's own template set so detail.tmpl can call {{template "logline" ...}}
// and sendLines can render a streamed line through the identical definition
// (docs/prds/prd-fleet-view.md § One template renders a log line).
var _ = template.Must(page.New("logline").Parse(loglineSource))

type logLineData struct {
	Kind   string
	Tool   string
	Detail string
	Anchor bool
}

const hiddenFirstFailAnchor = template.HTML(`<span id="first-fail"></span>`)

// renderLogLine is the one place an agentlog.Event becomes markup: the detail render and the SSE
// stream both call it, so the same event line reads identically wherever it arrived from.
func renderLogLine(e agentlog.Event, anchor bool) (template.HTML, error) {
	data := logLineData{Kind: e.Kind.String(), Tool: flatten(e.Tool), Detail: flatten(e.Detail), Anchor: anchor}
	var buf strings.Builder
	if err := page.ExecuteTemplate(&buf, "logline", data); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

// flatten keeps a rendered line on one physical line: the SSE wire format is one "data:" line
// per event, and a tool's own input can carry an embedded newline (a multi-line Bash command,
// for one).
func flatten(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}

// kindShown decides, for one of the four ?log= modes, whether an event's kind renders as a line.
// Phase headers always render regardless of mode; this only gates the lines inside them.
func kindShown(mode string, k agentlog.Kind) bool {
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

// phaseView is one agentlog.Phase, its events already filtered and rendered.
type phaseView struct {
	Skill string
	Note  string
	At    string
	Lines []template.HTML
}

type logFilterLink struct {
	Label  string
	Path   string
	Active bool
}

// logDetail is the selected row's run log, parsed and filtered, ready for detail.tmpl.
type logDetail struct {
	Path       string
	Streaming  bool
	Lines      int
	PhaseCount int
	Phases     []phaseView
	// Result is the closing line's text, empty until the run has one.
	Result     string
	Filters    []logFilterLink
	StreamPath string
}

// buildLogDetail parses path's whole run and renders it under mode, ready for the selected row's
// detail panel. streaming names whether the loop still owns this run, for the header's own
// status pill.
func buildLogDetail(path string, streaming bool, ticketURL string, params viewParams) logDetail {
	detail := logDetail{Path: path, Streaming: streaming, Filters: filterLinks(params)}

	whole, resumeAt := wholeLines(path)
	detail.StreamPath = logStreamPath(ticketURL, resumeAt, params.Log)
	if whole == nil {
		return detail
	}

	run, _ := agentlog.Parse(bytes.NewReader(whole))
	detail.Lines = run.Lines
	detail.PhaseCount = len(run.Phases)
	detail.Result = formatResult(run.Result)
	detail.Phases = renderPhases(run.Phases, params.Log)
	return detail
}

// wholeLines reads path's complete lines only: an agent flushes mid-line, so parsing a trailing
// partial one would read a line that has not finished arriving. The byte count returned is where
// the live SSE tail resumes from, so nothing rendered here is ever sent twice.
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

func renderPhases(phases []agentlog.Phase, mode string) []phaseView {
	firstFail := firstFailureIndex(phases)
	idx := 0
	views := make([]phaseView, len(phases))
	for i, phase := range phases {
		pv := phaseView{Skill: phase.Skill, Note: phase.Note, At: formatOffset(phase.At)}
		for _, event := range phase.Events {
			anchor := idx == firstFail
			idx++
			if !kindShown(mode, event.Kind) {
				if anchor {
					pv.Lines = append(pv.Lines, hiddenFirstFailAnchor)
				}
				continue
			}
			line, err := renderLogLine(event, anchor)
			if err != nil {
				continue
			}
			pv.Lines = append(pv.Lines, line)
		}
		views[i] = pv
	}
	return views
}

// firstFailureIndex finds the first Fail event's position in run order, across every phase and
// regardless of the current filter, so the anchor lands on the run's actual first failure and
// not merely the first one a filtered view happens to show.
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

func filterLinks(params viewParams) []logFilterLink {
	links := make([]logFilterLink, len(logFilters))
	for i, mode := range logFilters {
		links[i] = logFilterLink{Label: mode, Path: params.withLog(mode).pagePath(), Active: params.Log == mode}
	}
	return links
}

func formatResult(r *agentlog.Result) string {
	if r == nil {
		return ""
	}
	return fmt.Sprintf("%s · %s · %d turns · $%.2f", r.Outcome, formatDuration(r.Duration), r.Turns, r.CostUSD)
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	return fmt.Sprintf("%dm %ds", m, s)
}

// formatOffset is a phase's own start time, relative to the run's first event (§ Acceptance
// criteria: "timestamps relative to the first event and monotonic").
func formatOffset(d time.Duration) string {
	d = d.Round(time.Second)
	m := d / time.Minute
	s := d % time.Minute / time.Second
	return fmt.Sprintf("+%02d:%02d", m, s)
}
