package view

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

const hiddenFirstFailAnchor = `<span id="first-fail"></span>`

// LineRenderer turns one log event into the markup of its line. The anchor flag asks for the
// first-failure id.
type LineRenderer func(e agentlog.Event, anchor bool) (string, error)

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
	Skill string   `json:"skill"`
	Note  string   `json:"note"`
	At    string   `json:"at"`
	Lines []string `json:"lines"`
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
	Result     string          `json:"result"`
	Filters    []logFilterLink `json:"filters"`
	StreamPath string          `json:"stream_path"`
}

func buildLogDetail(render LineRenderer, path string, streaming bool, ticketURL string, params Params) LogDetail {
	detail := LogDetail{Path: path, Streaming: streaming, Filters: filterLinks(params)}

	whole, resumeAt := wholeLines(path)
	detail.StreamPath = logStreamPath(ticketURL, resumeAt, params.Log)
	if whole == nil {
		return detail
	}

	run, _ := agentlog.Parse(bytes.NewReader(whole))
	detail.Lines = run.Lines
	detail.PhaseCount = len(run.Phases)
	detail.Result = formatResult(run.Result)
	detail.Phases = renderPhases(render, run.Phases, params.Log)
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

func renderPhases(render LineRenderer, phases []agentlog.Phase, mode string) []phaseView {
	firstFail := firstFailureIndex(phases)
	idx := 0
	views := make([]phaseView, len(phases))
	for i, phase := range phases {
		pv := phaseView{Skill: phase.Skill, Note: phase.Note, At: formatOffset(phase.At)}
		for _, event := range phase.Events {
			anchor := idx == firstFail
			idx++
			if !KindShown(mode, event.Kind) {
				if anchor {
					pv.Lines = append(pv.Lines, hiddenFirstFailAnchor)
				}
				continue
			}
			line, err := render(event, anchor)
			if err != nil {
				continue
			}
			pv.Lines = append(pv.Lines, line)
		}
		views[i] = pv
	}
	return views
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
