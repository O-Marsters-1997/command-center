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
	"slices"
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
	skillCalls := map[string]bool{}

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
		for _, event := range parsed.events() {
			if event.Kind == Skill && event.CallID != "" {
				skillCalls[event.CallID] = true
			}
			if event.Kind == Pass && skillCalls[event.CallID] {
				continue
			}
			if !parsed.Timestamp.IsZero() {
				if base.IsZero() {
					base = parsed.Timestamp
				}
				event.At = parsed.Timestamp.Sub(base)
				run.End = max(run.End, event.At)
			}
			run.append(event)
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
	Subtype       string         `json:"subtype"`
	DurationMS    int64          `json:"duration_ms"`
	NumTurns      int            `json:"num_turns"`
	CostUSD       *float64       `json:"total_cost_usd"`
	Usage         usage          `json:"usage"`
	RateLimitInfo *rateLimitInfo `json:"rate_limit_info"`
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

func (l logLine) events() []Event {
	var events []Event
	for _, block := range l.Message.Content {
		switch {
		case l.Type == "assistant" && block.Type == "tool_use":
			events = append(events, block.toolEvent())
		case l.Type == "assistant" && block.Type == "text" && l.ParentToolUseID == "":
			if say := strings.TrimSpace(block.Text); say != "" {
				events = append(events, Event{Kind: Say, Detail: say})
			}
		case l.Type == "user" && block.Type == "tool_result":
			events = append(events, block.resultEvent())
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
	if slices.Contains(fileTools, b.Name) {
		kind = File
	}
	event := Event{Kind: kind, Tool: b.Name, Detail: primaryInput(input), CallID: b.ID}
	if b.Name == "Edit" {
		event.Diff = lineDiff(text(input["old_string"]), text(input["new_string"]))
	}
	return event
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
