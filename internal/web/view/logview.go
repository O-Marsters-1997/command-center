package view

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/url"
	"os"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type LineRenderer func(LogLine) (template.HTML, error)

func KindShown(mode LogFilter, k agentlog.Kind) bool {
	switch mode {
	case LogSkills:
		return k == agentlog.Skill
	case LogTools:
		return k == agentlog.Tool || k == agentlog.File || k == agentlog.Cmd || k == agentlog.Commit
	case LogFails:
		return k == agentlog.Fail
	case LogFilterUnknown, LogAll:
	}
	return true
}

func EventShown(mode LogFilter, e agentlog.Event) bool {
	if e.Kind == agentlog.Cmd && mode == LogFails {
		return cmdFailed(e)
	}
	return KindShown(mode, e.Kind)
}

type phaseView struct {
	Skill string    `json:"skill"`
	Note  string    `json:"note"`
	At    string    `json:"at"`
	Items []logItem `json:"items"`
}

type logItem struct {
	Kind      string        `json:"kind"`
	Line      template.HTML `json:"line,omitempty"`
	Group     callGroup     `json:"group"`
	Folded    bool          `json:"-"`
	FirstFail bool          `json:"-"`
}

type callGroup struct {
	Summary  string          `json:"summary"`
	FailNote string          `json:"fail_note"`
	Open     bool            `json:"open"`
	Lines    []template.HTML `json:"lines"`
}

type LogDetail struct {
	Path       string        `json:"path"`
	Streaming  bool          `json:"streaming"`
	Lines      int           `json:"lines"`
	PhaseCount int           `json:"phase_count"`
	Phases     []phaseView   `json:"phases"`
	StreamPath string        `json:"stream_path"`
	Worked     workedLine    `json:"worked"`
	Final      template.HTML `json:"final"`
}

type LogSource struct {
	Path      string
	Streaming bool
	TicketURL string
	Records   []store.Event
}

func BuildLogDetail(render LineRenderer, src LogSource, params Params) (LogDetail, error) {
	path, streaming, ticketURL, records := src.Path, src.Streaming, src.TicketURL, src.Records
	detail := LogDetail{Path: path, Streaming: streaming}
	records = slices.DeleteFunc(slices.Clone(records), func(e store.Event) bool { return !RecordShown(e) })

	whole, resumeAt, err := wholeLines(path)
	if err != nil {
		return LogDetail{}, fmt.Errorf("load whole lines: %w", err)
	}
	detail.StreamPath = logStreamPath(ticketURL, resumeAt, lastRecordID(records), params.Log)
	if whole == nil && len(records) == 0 {
		return detail, nil
	}

	var run agentlog.Run
	if whole != nil {
		if run, err = agentlog.Parse(bytes.NewReader(whole)); err != nil {
			return LogDetail{}, fmt.Errorf("parse agent log %s: %w", path, err)
		}
	}
	final := takeFinalAnswer(&run)
	stats := measure(run)
	mode := params.Log

	detail.Lines = run.Lines
	detail.PhaseCount = len(run.Phases)
	detail.Worked = stats.worked()
	mergeRecords(&run, records)
	if detail.Phases, err = renderPhases(render, run.Phases, mode); err != nil {
		return LogDetail{}, fmt.Errorf("render phases: %w", err)
	}
	if final != "" {
		if detail.Final, err = render(sayLine(final)); err != nil {
			return LogDetail{}, fmt.Errorf("render final answer: %w", err)
		}
	}
	return detail, nil
}

func RecordShown(e store.Event) bool {
	return e.Kind == store.EventPRMerged || e.Kind == store.EventPROpened || e.Kind == store.EventChecksLanded
}

func RecordOf(e store.Event) agentlog.Event {
	return agentlog.Event{Kind: agentlog.Record, Tool: string(e.Kind), Detail: e.Detail}
}

func lastRecordID(records []store.Event) int64 {
	var last int64
	for _, r := range records {
		last = max(last, r.ID)
	}
	return last
}

func mergeRecords(run *agentlog.Run, records []store.Event) {
	for _, r := range records {
		if len(run.Phases) == 0 {
			run.Phases = append(run.Phases, agentlog.Phase{})
		}
		record := RecordOf(r)
		if !run.Start.IsZero() {
			record.At = r.At.Sub(run.Start)
		}
		phaseIdx := 0
		for i, p := range run.Phases {
			if p.At <= record.At {
				phaseIdx = i
			}
		}
		phase := &run.Phases[phaseIdx]
		at := slices.IndexFunc(phase.Events, func(e agentlog.Event) bool { return e.At > record.At })
		if at < 0 {
			at = len(phase.Events)
		}
		phase.Events = slices.Insert(phase.Events, at, record)
	}
}

func wholeLines(path string) ([]byte, int64, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read agent log %s: %w", path, err)
	}
	// External agent log writers may leave a partial line; the SSE tail must receive it instead.
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return nil, 0, nil
	}
	return data[:end+1], int64(end + 1), nil
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

func renderPhases(render LineRenderer, phases []agentlog.Phase, mode LogFilter) ([]phaseView, error) {
	firstFail := firstFailureIndex(phases)
	openGroups := mode == LogTools || mode == LogFails
	idx := 0
	var views []phaseView
	for _, phase := range phases {
		b := phaseBuilder{render: render, mode: mode, openGroups: openGroups}
		for _, event := range phase.Events {
			if err := b.add(event, idx == firstFail); err != nil {
				return nil, fmt.Errorf("add event: %w", err)
			}
			idx++
		}
		if err := b.flush(); err != nil {
			return nil, fmt.Errorf("flush: %w", err)
		}
		views = append(views, phaseView{Skill: phase.Skill, Note: phase.Note, At: formatOffset(phase.At), Items: b.items})
	}
	return views, nil
}

type phaseBuilder struct {
	render     LineRenderer
	mode       LogFilter
	openGroups bool
	items      []logItem
	pending    []callPair
}

func (b *phaseBuilder) add(event agentlog.Event, anchor bool) error {
	switch event.Kind {
	case agentlog.Say:
		if err := b.flush(); err != nil {
			return fmt.Errorf("flush: %w", err)
		}
		if !KindShown(b.mode, event.Kind) {
			return nil
		}
		line, err := b.render(sayLine(event.Detail))
		if err != nil {
			return fmt.Errorf("render say line: %w", err)
		}
		b.items = append(b.items, logItem{Kind: "say", Line: line})
	case agentlog.Record, agentlog.Commit:
		if err := b.flush(); err != nil {
			return fmt.Errorf("flush: %w", err)
		}
		if !KindShown(b.mode, event.Kind) {
			return nil
		}
		line, err := b.render(LineOf(event))
		if err != nil {
			return fmt.Errorf("render %s line: %w", event.Kind, err)
		}
		b.items = append(b.items, logItem{Kind: event.Kind.String(), Line: line})
	case agentlog.Pass, agentlog.Fail:
		b.answer(event, anchor)
	case agentlog.Cmd:
		b.pending = append(b.pending, cmdPair(event, anchor))
	case agentlog.Skill, agentlog.File, agentlog.Tool:
		b.pending = append(b.pending, callPair{call: event, called: true})
	}
	return nil
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

func (b *phaseBuilder) flush() error {
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
				return fmt.Errorf("render call line: %w", err)
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
		b.items = append(b.items, logItem{Kind: "calls", Group: group, Folded: true})
	}
	if hiddenAnchor {
		b.items = append(b.items, logItem{Kind: "anchor", FirstFail: true})
	}
	return nil
}

func firstFailureIndex(phases []agentlog.Phase) int {
	idx := 0
	for _, phase := range phases {
		for _, e := range phase.Events {
			if failedEvent(e) {
				return idx
			}
			idx++
		}
	}
	return -1
}

func formatOffset(d time.Duration) string {
	d = d.Round(time.Second)
	m := d / time.Minute
	s := d % time.Minute / time.Second
	return fmt.Sprintf("+%02d:%02d", m, s)
}

func logStreamPath(ticketURL string, from, afterRecord int64, mode LogFilter) string {
	path := fmt.Sprintf("/ticket/%s/log?from=%d&records=%d", url.PathEscape(ticketURL), from, afterRecord)
	if mode != LogFilterUnknown && mode != LogAll {
		path += "&log=" + url.QueryEscape(string(mode))
	}
	return path
}
