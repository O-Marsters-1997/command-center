// Package agentlog reads a run's log — the agent CLI's stream-json, one object per line — into
// the phases, tool lines and result a person can read without opening the JSON. It is pure: an
// io.Reader in, values out, no store and no clock.
package agentlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Kind is what a kept line is to a reader: the skill that opens a phase, a file touched, any
// other tool call, the two outcomes a tool result carries, and the agent's own prose.
type Kind int

const (
	Skill Kind = iota
	File
	Tool
	Fail
	Pass
	Say
	Cmd
)

func (k Kind) String() string {
	switch k {
	case Skill:
		return "skill"
	case File:
		return "file"
	case Tool:
		return "tool"
	case Fail:
		return "fail"
	case Pass:
		return "pass"
	case Say:
		return "say"
	case Cmd:
		return "cmd"
	default:
		return "unknown"
	}
}

// Event is one kept line. At is measured off the run's first kept event, so it is zero on an
// Event from ParseLine, which has no run to measure against.
type Event struct {
	At          time.Duration
	Kind        Kind
	Tool        string
	Detail      string
	CallID      string
	Output      string
	OutputLines int
	Diff        Diff
	Done        bool
	ExitCode    int
	Interrupted bool
	Elapsed     time.Duration
}

// Phase is the work between one Skill tool use and the next. A run's first phase has no skill:
// everything before the agent loaded one belongs to it.
type Phase struct {
	Skill  string
	Note   string
	At     time.Duration
	Events []Event
	Turns  int
	Spend  float64
}

// Result is the run's own last word on itself, taken from the final result line.
type Result struct {
	Outcome  string
	Duration time.Duration
	Turns    int
	CostUSD  float64
}

// Run is one log read whole: its phases in order, the result once the run has ended, and the
// count of whole lines behind them.
type Run struct {
	Phases []Phase
	Result *Result
	Lines  int
	End    time.Duration
}

// Parse reads a log into phases. A run that is still writing ends in a partial line, which is
// left out rather than failing the read, so a live log parses as the lines already whole.
func Parse(r io.Reader) (Run, error) {
	var run Run
	var base time.Time
	var ledger requestLedger
	var tail Tail
	callPhase := map[string]int{}

	err := eachLine(r, func(line []byte) {
		run.Lines++

		parsed, decodeErr := decode(line)
		if decodeErr != nil {
			return
		}
		if parsed.Type == "result" {
			result := parsed.result()
			run.Result = &result
			return
		}
		for _, event := range tail.feed(parsed) {
			if !parsed.Timestamp.IsZero() {
				if base.IsZero() {
					base = parsed.Timestamp
				}
				event.At = parsed.Timestamp.Sub(base)
				run.End = max(run.End, event.At)
			}
			if event.Kind == Cmd && event.Done {
				phase, ok := callPhase[event.CallID]
				if !ok {
					phase = len(run.Phases) - 1
				}
				if phase >= 0 && run.Phases[phase].answerCmd(event) {
					continue
				}
			}
			if phase, ok := callPhase[event.CallID]; ok && (event.Kind == Pass || event.Kind == Fail) {
				run.Phases[phase].Events = append(run.Phases[phase].Events, event)
				continue
			}
			run.append(event)
			if event.CallID != "" && event.Kind != Skill {
				callPhase[event.CallID] = len(run.Phases) - 1
			}
		}
		ledger.note(parsed, len(run.Phases)-1)
	})
	ledger.settle(run.Phases)
	return run, err
}

// ParseLine reads one log line into its events, none for a line no reader wants: a system line,
// a rate limit, the agent's thinking, and anything it cannot decode.
func ParseLine(line []byte) []Event {
	parsed, err := decode(line)
	if err != nil {
		return nil
	}
	return parsed.events()
}

func (p *Phase) answerCmd(done Event) bool {
	for i, e := range p.Events {
		if e.Kind == Cmd && !e.Done && e.CallID == done.CallID {
			done.At = e.At
			p.Events[i] = done
			return true
		}
	}
	return false
}

func (run *Run) append(event Event) {
	if event.Kind == Skill {
		run.Phases = append(run.Phases, Phase{Skill: event.Tool, Note: event.Detail, At: event.At})
		return
	}
	if len(run.Phases) == 0 {
		run.Phases = append(run.Phases, Phase{})
	}
	phase := &run.Phases[len(run.Phases)-1]
	phase.Events = append(phase.Events, event)
}

type logLine struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"request_id"`
	// ParentToolUseID names the tool_use id of the Task call that spawned this line's thread, and
	// is empty on every main-thread line.
	ParentToolUseID string `json:"parent_tool_use_id"`
	Message         struct {
		Model   string         `json:"model"`
		Content []contentBlock `json:"content"`
		Usage   usage          `json:"usage"`
	} `json:"message"`
	ToolUseResult json.RawMessage `json:"tool_use_result"`
	Subtype       string          `json:"subtype"`
	DurationMS    int64           `json:"duration_ms"`
	NumTurns      int             `json:"num_turns"`
	CostUSD       *float64        `json:"total_cost_usd"`
	Usage         usage           `json:"usage"`
	RateLimitInfo *rateLimitInfo  `json:"rate_limit_info"`
}

type rateLimitInfo struct {
	UnifiedWindows map[string]struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    int64   `json:"resetsAt"`
	} `json:"unifiedWindows"`
}

type usage struct {
	Input       int `json:"input_tokens"`
	CacheCreate int `json:"cache_creation_input_tokens"`
	CacheRead   int `json:"cache_read_input_tokens"`
	Output      int `json:"output_tokens"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Name      string          `json:"name"`
	Text      string          `json:"text"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func decode(line []byte) (logLine, error) {
	var parsed logLine
	err := json.Unmarshal(line, &parsed)
	return parsed, err
}

func forEachLine(logPath string, fn func(logLine, time.Time)) error {
	f, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("open agent log %s: %w", logPath, err)
	}
	defer func() { _ = f.Close() }()

	var last time.Time
	err = eachLine(f, func(line []byte) {
		parsed, decodeErr := decode(line)
		if decodeErr != nil {
			return
		}
		if !parsed.Timestamp.IsZero() {
			last = parsed.Timestamp
		}
		fn(parsed, last)
	})
	if err != nil {
		return fmt.Errorf("read agent log %s: %w", logPath, err)
	}
	return nil
}

func eachLine(r io.Reader, fn func([]byte)) error {
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		fn(line)
	}
}

func (l logLine) result() Result {
	var cost float64
	if l.CostUSD != nil {
		cost = *l.CostUSD
	}
	return Result{
		Outcome:  l.Subtype,
		Duration: time.Duration(l.DurationMS) * time.Millisecond,
		Turns:    l.NumTurns,
		CostUSD:  cost,
	}
}

// Tail reads a live log one line at a time, dropping the results that only announce a Skill
// launch, as Parse does.
type Tail struct {
	skillCalls map[string]bool
	cmdCalls   map[string]cmdCall
}

type cmdCall struct {
	command string
	at      time.Time
}

func (t *Tail) Read(line []byte) []Event {
	parsed, err := decode(line)
	if err != nil {
		return nil
	}
	return t.feed(parsed)
}

func (t *Tail) feed(l logLine) []Event {
	events := l.events()
	for i, e := range events {
		switch e.Kind {
		case Cmd:
			if t.cmdCalls == nil {
				t.cmdCalls = make(map[string]cmdCall)
			}
			t.cmdCalls[e.CallID] = cmdCall{command: e.Detail, at: l.Timestamp}
		case Pass, Fail:
			if call, ok := t.cmdCalls[e.CallID]; ok {
				events[i] = l.cmdDone(call, e.CallID)
				delete(t.cmdCalls, e.CallID)
			}
		default:
		}
	}
	return t.keep(events)
}

func (t *Tail) keep(events []Event) []Event {
	return slices.DeleteFunc(events, func(e Event) bool {
		if e.Kind == Skill && e.CallID != "" {
			if t.skillCalls == nil {
				t.skillCalls = make(map[string]bool)
			}
			t.skillCalls[e.CallID] = true
		}
		return e.Kind == Pass && t.skillCalls[e.CallID]
	})
}

func (l logLine) events() []Event {
	var events []Event
	for _, block := range l.Message.Content {
		switch {
		case l.Type == "assistant" && block.Type == "tool_use":
			event := block.toolEvent()
			if event.Kind == Skill && l.ParentToolUseID != "" {
				event = Event{Kind: Tool, Tool: block.Name, Detail: event.Tool, CallID: event.CallID}
			}
			events = append(events, event)
		case l.Type == "assistant" && block.Type == "text" && l.ParentToolUseID == "":
			if say := strings.TrimSpace(block.Text); say != "" {
				events = append(events, Event{Kind: Say, Detail: say})
			}
		case l.Type == "user" && block.Type == "tool_result":
			event := block.resultEvent()
			if len(l.Message.Content) == 1 {
				event.Diff = changeCounts(l.ToolUseResult)
			}
			events = append(events, event)
		}
	}
	return events
}

var fileTools = []string{"Read", "Write", "Edit", "NotebookEdit"}

func (b contentBlock) toolEvent() Event {
	var input map[string]any
	_ = json.Unmarshal(b.Input, &input)

	if b.Name == "Skill" {
		return Event{Kind: Skill, Tool: text(input["skill"]), Detail: text(input["args"]), CallID: b.ID}
	}
	kind := Tool
	switch {
	case b.Name == "Bash":
		kind = Cmd
	case slices.Contains(fileTools, b.Name):
		kind = File
	}
	event := Event{Kind: kind, Tool: b.Name, Detail: primaryInput(input), CallID: b.ID}
	switch b.Name {
	case "Edit":
		event.Diff = lineDiff(text(input["old_string"]), text(input["new_string"]))
	case "Write":
		event.Diff = Diff{Added: countLines(text(input["content"]))}
	}
	return event
}

func changeCounts(raw json.RawMessage) Diff {
	var result struct {
		Type            string `json:"type"`
		Content         string `json:"content"`
		StructuredPatch []struct {
			Lines []string `json:"lines"`
		} `json:"structuredPatch"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return Diff{}
	}
	if result.Type == "create" {
		return Diff{Added: countLines(result.Content)}
	}
	var counts Diff
	for _, hunk := range result.StructuredPatch {
		for _, line := range hunk.Lines {
			switch {
			case strings.HasPrefix(line, "+"):
				counts.Added++
			case strings.HasPrefix(line, "-"):
				counts.Removed++
			}
		}
	}
	return counts
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}

func (b contentBlock) resultEvent() Event {
	kind := Pass
	if b.IsError {
		kind = Fail
	}
	whole := strings.TrimRight(b.resultText(), "\n")
	output, lines := boundOutput(whole)
	return Event{Kind: kind, Detail: firstLine(whole), CallID: b.ToolUseID, Output: output, OutputLines: lines}
}

var exitCodePrefix = regexp.MustCompile(`^(?:Error: )?Exit code (\d+)\n?`)

func (l logLine) cmdDone(call cmdCall, callID string) Event {
	var block contentBlock
	results := 0
	for _, b := range l.Message.Content {
		if b.Type != "tool_result" {
			continue
		}
		results++
		if b.ToolUseID == callID {
			block = b
		}
	}

	done := Event{Kind: Cmd, Tool: "Bash", Detail: call.command, CallID: callID, Done: true}
	var output string
	if results == 1 {
		var failure string
		var bash struct {
			Stdout      string `json:"stdout"`
			Stderr      string `json:"stderr"`
			Interrupted bool   `json:"interrupted"`
		}
		switch {
		case json.Unmarshal(l.ToolUseResult, &failure) == nil:
			output = failure
		case json.Unmarshal(l.ToolUseResult, &bash) == nil:
			output = joinNonEmpty(bash.Stdout, bash.Stderr)
			done.Interrupted = bash.Interrupted
		}
	}
	if output == "" {
		output = block.resultText()
	}
	if block.IsError {
		done.ExitCode = 1
		if match := exitCodePrefix.FindStringSubmatch(output); match != nil {
			done.ExitCode, _ = strconv.Atoi(match[1])
			output = output[len(match[0]):]
		}
	}
	if !l.Timestamp.IsZero() && !call.at.IsZero() {
		done.Elapsed = max(0, l.Timestamp.Sub(call.at))
	}
	done.Output, done.OutputLines = boundOutput(strings.TrimRight(output, "\n"))
	return done
}

func joinNonEmpty(parts ...string) string {
	return strings.Join(slices.DeleteFunc(slices.Clone(parts), func(p string) bool { return p == "" }), "\n")
}

// The CLI writes a tool result's content either as a bare string or as Messages API blocks.
func (b contentBlock) resultText() string {
	var whole string
	if json.Unmarshal(b.Content, &whole) == nil {
		return whole
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(b.Content, &blocks) != nil {
		return ""
	}
	texts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		texts = append(texts, block.Text)
	}
	return strings.Join(texts, "\n")
}

const (
	maxOutputLines = 40
	maxOutputBytes = 4000
)

func boundOutput(whole string) (string, int) {
	if whole == "" {
		return "", 0
	}
	lines := strings.Split(whole, "\n")
	kept := strings.Join(lines[:min(len(lines), maxOutputLines)], "\n")
	if len(kept) > maxOutputBytes {
		kept = strings.ToValidUTF8(kept[:maxOutputBytes], "")
	}
	if kept != whole {
		kept += "\n… output cut short"
		if len(lines) > maxOutputLines {
			kept += fmt.Sprintf(", %d more lines", len(lines)-maxOutputLines)
		}
	}
	return kept, len(lines)
}

// Ordered most specific first: Bash input has both command and description.
var primaryInputKeys = []string{"command", "file_path", "pattern", "path", "url", "query", "description", "prompt"}

func primaryInput(input map[string]any) string {
	for _, key := range primaryInputKeys {
		if value := text(input[key]); value != "" {
			return value
		}
	}
	return ""
}

func text(value any) string {
	s, _ := value.(string)
	return s
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimRight(line, "\r")
}
