// Package agentlog reads a run's log — the agent CLI's stream-json, one object per line — into
// the phases, tool lines and result a person can read without opening the JSON. It is pure: an
// io.Reader in, values out, no store and no clock.
package agentlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"
)

// Kind is what a kept line is to a reader: the skill that opens a phase, a file touched, any
// other tool call, and the two outcomes a tool result carries.
type Kind int

const (
	Skill Kind = iota
	File
	Tool
	Fail
	Pass
)

// Event is one kept line. At is measured off the run's first kept event, so it is zero on an
// Event from ParseLine, which has no run to measure against.
type Event struct {
	At     time.Duration
	Kind   Kind
	Tool   string
	Detail string
}

// Phase is the work between one Skill tool use and the next. A run's first phase has no skill:
// everything before the agent loaded one belongs to it.
type Phase struct {
	Skill  string
	Note   string
	At     time.Duration
	Events []Event
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
}

// Parse reads a log into phases. A run that is still writing ends in a partial line, which is
// left out rather than failing the read, so a live log parses as the lines already whole.
func Parse(r io.Reader) (Run, error) {
	var run Run
	var base time.Time

	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return run, nil
			}
			return run, err
		}
		run.Lines++

		parsed, decodeErr := decode(line)
		if decodeErr != nil {
			continue
		}
		if parsed.Type == "result" {
			result := parsed.result()
			run.Result = &result
			continue
		}
		event, at, ok := parsed.event()
		if !ok {
			continue
		}
		if !at.IsZero() {
			if base.IsZero() {
				base = at
			}
			event.At = at.Sub(base)
		}
		run.append(event)
	}
}

// ParseLine reads one log line, reporting false for a line no reader wants: a system line, a
// rate limit, the agent's thinking or prose, and anything it cannot decode.
func ParseLine(line []byte) (Event, bool) {
	parsed, err := decode(line)
	if err != nil {
		return Event{}, false
	}
	event, _, ok := parsed.event()
	return event, ok
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
	Message   struct {
		Content []contentBlock `json:"content"`
		Usage   usage          `json:"usage"`
	} `json:"message"`
	Subtype    string  `json:"subtype"`
	DurationMS int64   `json:"duration_ms"`
	NumTurns   int     `json:"num_turns"`
	CostUSD    float64 `json:"total_cost_usd"`
}

type usage struct {
	Input       int `json:"input_tokens"`
	CacheCreate int `json:"cache_creation_input_tokens"`
	CacheRead   int `json:"cache_read_input_tokens"`
	Output      int `json:"output_tokens"`
}

type contentBlock struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"is_error"`
}

func decode(line []byte) (logLine, error) {
	var parsed logLine
	err := json.Unmarshal(line, &parsed)
	return parsed, err
}

func (l logLine) result() Result {
	return Result{
		Outcome:  l.Subtype,
		Duration: time.Duration(l.DurationMS) * time.Millisecond,
		Turns:    l.NumTurns,
		CostUSD:  l.CostUSD,
	}
}

func (l logLine) event() (Event, time.Time, bool) {
	for _, block := range l.Message.Content {
		switch {
		case l.Type == "assistant" && block.Type == "tool_use":
			return block.toolEvent(), l.Timestamp, true
		case l.Type == "user" && block.Type == "tool_result":
			kind := Pass
			if block.IsError {
				kind = Fail
			}
			return Event{Kind: kind, Detail: firstLine(block.resultText())}, l.Timestamp, true
		}
	}
	return Event{}, time.Time{}, false
}

var fileTools = []string{"Read", "Write", "Edit", "NotebookEdit"}

func (b contentBlock) toolEvent() Event {
	var input map[string]any
	_ = json.Unmarshal(b.Input, &input)

	if b.Name == "Skill" {
		return Event{Kind: Skill, Tool: text(input["skill"]), Detail: text(input["args"])}
	}
	kind := Tool
	if slices.Contains(fileTools, b.Name) {
		kind = File
	}
	return Event{Kind: kind, Tool: b.Name, Detail: primaryInput(input)}
}

// resultText reads a tool result's content, which the CLI writes either as a bare string or as
// the Messages API's content blocks.
func (b contentBlock) resultText() string {
	var whole string
	if json.Unmarshal(b.Content, &whole) == nil {
		return whole
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(b.Content, &blocks) != nil || len(blocks) == 0 {
		return ""
	}
	return blocks[0].Text
}

// primaryInputKeys is ordered most specific first because a tool's input schema carries several
// of them at once: Bash has both command and description, and the command is the one to show.
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
