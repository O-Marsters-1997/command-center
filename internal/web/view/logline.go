package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

// LogLine is one line of the run log as logline.tmpl renders it: the agent's prose, a skill, or
// one tool call alongside the result that answered it.
type LogLine struct {
	Kind    string
	Blocks  []ProseBlock
	Verb    string
	Arg     string
	Result  string
	Failed  bool
	Output  string
	Fold    bool
	Open    bool
	Diff    []DiffRow
	Added   int
	Removed int
	Anchor  bool
	CallID  string
	Replace bool
}

type ProseBlock struct {
	List  bool
	Items [][]ProseSpan
}

type ProseSpan struct {
	Text         string
	Code, Strong bool
}

type DiffRow struct {
	Op, Text string
}

// LineOf is the line for one event read alone, as the live tail reads it: a call and its result
// arrive on separate lines, so each renders as its own row.
func LineOf(e agentlog.Event) LogLine {
	switch e.Kind {
	case agentlog.Say:
		return sayLine(e.Detail)
	case agentlog.Skill:
		return LogLine{Kind: e.Kind.String(), Verb: e.Tool, Arg: e.Detail}
	case agentlog.Record:
		return recordLine(e.Tool, e.Detail)
	case agentlog.Pass, agentlog.Fail:
		return callPair{result: e, answered: true}.line()
	case agentlog.Cmd:
		return cmdPair(e, false).line()
	default:
		return callPair{call: e, called: true}.line()
	}
}

func recordLine(kind, detail string) LogLine {
	switch kind {
	case "pr_merged":
		return LogLine{Kind: agentlog.Record.String(), Verb: "merged", Arg: detail}
	default:
		return LogLine{Kind: agentlog.Record.String(), Verb: kind, Arg: detail}
	}
}

type callPair struct {
	call, result     agentlog.Event
	called, answered bool
	anchor           bool
}

func cmdPair(e agentlog.Event, anchor bool) callPair {
	return callPair{call: e, result: e, called: true, answered: e.Done, anchor: anchor}
}

func cmdFailed(e agentlog.Event) bool {
	return e.Kind == agentlog.Cmd && e.Done && (e.ExitCode != 0 || e.Interrupted)
}

func failedEvent(e agentlog.Event) bool { return e.Kind == agentlog.Fail || cmdFailed(e) }

func (p callPair) failed() bool { return p.answered && failedEvent(p.result) }

func (p callPair) shown(mode string) bool {
	return (p.called && EventShown(mode, p.call)) || (p.answered && EventShown(mode, p.result))
}

func (p callPair) cmdWord() string {
	switch {
	case !p.answered:
		return "running"
	case p.result.Interrupted:
		return "interrupted" + elapsedSuffix(p.result.Elapsed)
	case p.result.ExitCode != 0:
		return fmt.Sprintf("exit %d%s", p.result.ExitCode, elapsedSuffix(p.result.Elapsed))
	default:
		return "✓" + elapsedSuffix(p.result.Elapsed)
	}
}

func elapsedSuffix(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d < time.Second {
		return " " + d.Round(time.Millisecond).String()
	}
	return " " + d.Round(100*time.Millisecond).String()
}

func (p callPair) line() LogLine {
	line := LogLine{Kind: p.kind(), Failed: p.failed(), Anchor: p.anchor, Verb: "Result", Arg: p.result.Detail}
	if p.called {
		line.Verb = verbOf(p.call.Tool)
		line.Arg = p.call.Detail
		line.Added, line.Removed = p.call.Diff.Added, p.call.Diff.Removed
		for _, row := range p.call.Diff.Lines {
			line.Diff = append(line.Diff, DiffRow{Op: row.Op.String(), Text: row.Text})
		}
	}
	if p.called && p.call.Kind == agentlog.Cmd {
		line.CallID = p.call.CallID
		line.Result = p.cmdWord()
		line.Output = p.result.Output
		line.Fold = line.Output != ""
		line.Open = line.Fold && line.Failed
		return line
	}
	if p.called && p.answered {
		if counted := p.result.Diff; counted.Added+counted.Removed > 0 {
			line.Added, line.Removed = counted.Added, counted.Removed
		}
	}
	if p.answered {
		line.Result = p.resultWord()
		if line.Failed || categoryOf(p.call.Tool) == running {
			line.Output = p.result.Output
		}
	}
	return line
}

func (p callPair) kind() string {
	switch {
	case p.failed():
		return agentlog.Fail.String()
	case p.called:
		return p.call.Kind.String()
	default:
		return agentlog.Pass.String()
	}
}

func (p callPair) resultWord() string {
	if p.failed() {
		return "failed"
	}
	switch categoryOf(p.call.Tool) {
	case reading:
		return plural(p.result.OutputLines, "line", "lines")
	case searching:
		return plural(p.result.OutputLines, "match", "matches")
	case editing:
		return ""
	case running:
		return "ok"
	default:
		return "done"
	}
}

type toolCategory int

const (
	other toolCategory = iota
	reading
	searching
	editing
	running
)

func categoryOf(tool string) toolCategory {
	switch tool {
	case "Read":
		return reading
	case "Grep", "Glob", "WebSearch":
		return searching
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		return editing
	case "Bash":
		return running
	default:
		return other
	}
}

func verbOf(tool string) string {
	switch categoryOf(tool) {
	case searching:
		return "Search"
	case running:
		return "Ran"
	default:
		return tool
	}
}

type toolMix [running + 1]int

func mixOf(pairs []callPair) toolMix {
	var mix toolMix
	for _, p := range pairs {
		if p.called {
			mix[categoryOf(p.call.Tool)]++
		}
	}
	return mix
}

func (m toolMix) summary() string {
	var parts []string
	if n := m[reading]; n > 0 {
		parts = append(parts, "read "+plural(n, "file", "files"))
	}
	if n := m[searching]; n > 0 {
		parts = append(parts, "searched "+times(n))
	}
	if n := m[editing]; n > 0 {
		parts = append(parts, "edited "+plural(n, "file", "files"))
	}
	if n := m[running]; n > 0 {
		parts = append(parts, "ran "+plural(n, "command", "commands"))
	}
	if n := m[other]; n > 0 {
		noun := plural(n, "tool", "tools")
		if len(parts) > 0 {
			noun = plural(n, "other tool", "other tools")
		}
		parts = append(parts, "used "+noun)
	}
	if len(parts) == 0 {
		return "Tool results"
	}
	summary := strings.Join(parts, ", ")
	return strings.ToUpper(summary[:1]) + summary[1:]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func times(n int) string {
	switch n {
	case 1:
		return "once"
	case 2:
		return "twice"
	default:
		return fmt.Sprintf("%d times", n)
	}
}

func sayLine(text string) LogLine {
	return LogLine{Kind: agentlog.Say.String(), Blocks: proseBlocks(text)}
}

func proseBlocks(text string) []ProseBlock {
	var blocks []ProseBlock
	var paragraph []string
	var list [][]ProseSpan
	flushParagraph := func() {
		if len(paragraph) > 0 {
			blocks = append(blocks, ProseBlock{Items: [][]ProseSpan{spans(strings.Join(paragraph, " "))}})
			paragraph = nil
		}
	}
	flushList := func() {
		if len(list) > 0 {
			blocks = append(blocks, ProseBlock{List: true, Items: list})
			list = nil
		}
	}

	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			flushParagraph()
			flushList()
		case strings.HasPrefix(line, "- "), strings.HasPrefix(line, "* "):
			flushParagraph()
			list = append(list, spans(line[2:]))
		case strings.HasPrefix(line, "#"):
			flushParagraph()
			flushList()
			heading := strings.TrimSpace(strings.TrimLeft(line, "#"))
			blocks = append(blocks, ProseBlock{Items: [][]ProseSpan{{{Text: heading, Strong: true}}}})
		default:
			flushList()
			paragraph = append(paragraph, line)
		}
	}
	flushParagraph()
	flushList()
	return blocks
}

func spans(s string) []ProseSpan {
	var out []ProseSpan
	for s != "" {
		at, delim := nextDelimiter(s)
		if at < 0 {
			return append(out, ProseSpan{Text: s})
		}
		rest := s[at+len(delim):]
		end := strings.Index(rest, delim)
		if end <= 0 {
			out = append(out, ProseSpan{Text: s[:at+len(delim)]})
			s = rest
			continue
		}
		if at > 0 {
			out = append(out, ProseSpan{Text: s[:at]})
		}
		out = append(out, ProseSpan{Text: rest[:end], Code: delim == "`", Strong: delim == "**"})
		s = rest[end+len(delim):]
	}
	return out
}

func nextDelimiter(s string) (int, string) {
	tick := strings.Index(s, "`")
	bold := strings.Index(s, "**")
	switch {
	case tick < 0 && bold < 0:
		return -1, ""
	case bold < 0 || (tick >= 0 && tick < bold):
		return tick, "`"
	default:
		return bold, "**"
	}
}
