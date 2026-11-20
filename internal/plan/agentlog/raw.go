package agentlog

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const rawMalformed = "malformed"

type RawLine struct {
	N       int
	At      time.Time
	Type    string // the stream-json "type": system, assistant, user, result
	Tool    string
	Summary string
	Failed  bool
	JSON    string
}

func ParseRaw(r io.Reader) ([]RawLine, error) {
	var lines []RawLine
	tools := map[string]string{}
	n := 0
	err := eachLine(r, func(line []byte) {
		n++
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			return
		}
		lines = append(lines, rawLine(n, line, tools))
	})
	if err != nil {
		return lines, fmt.Errorf("read raw agent log: %w", err)
	}
	return lines, nil
}

func rawLine(n int, line []byte, tools map[string]string) RawLine {
	var head struct {
		Type      string    `json:"type"`
		Timestamp time.Time `json:"timestamp"`
		IsError   bool      `json:"is_error"`
	}
	if json.Unmarshal(line, &head) != nil {
		return RawLine{N: n, Type: rawMalformed, Summary: string(line), Failed: true, JSON: string(line)}
	}
	var indented bytes.Buffer
	if json.Indent(&indented, line, "", "  ") != nil {
		indented.Write(line)
	}
	raw := RawLine{N: n, At: head.Timestamp, Type: head.Type, JSON: indented.String()}
	parsed, err := decode(line)
	if err != nil {
		return raw
	}
	switch parsed.Type {
	case "system", "result":
		raw.Summary = parsed.Subtype
		raw.Failed = head.IsError || strings.HasPrefix(parsed.Subtype, "error")
		return raw
	}
	for _, block := range parsed.Message.Content {
		var summary string
		switch block.Type {
		case "text":
			summary = firstLine(strings.TrimSpace(block.Text))
		case "tool_use":
			tools[block.ID] = block.Name
			raw.Tool = cmp.Or(raw.Tool, block.Name)
			summary = block.toolEvent().Detail
		case "tool_result":
			raw.Tool = cmp.Or(raw.Tool, tools[block.ToolUseID])
			raw.Failed = raw.Failed || block.IsError
			summary = firstLine(strings.TrimSpace(block.resultText()))
		}
		raw.Summary = cmp.Or(raw.Summary, summary)
	}
	return raw
}
