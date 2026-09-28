package agentlog

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// RunMetrics is what every dialect must produce. A field a dialect cannot supply is nil or zero
// with Settled false, never a guess.
type RunMetrics struct {
	TokensIn     int64
	TokensOut    int64
	Turns        int
	Duration     time.Duration
	CostUSD      *float64
	ToolCalls    int
	ToolFailures int
	Model        string
	Settled      bool
	// Requests is one deduplicated request_id per element, in the order each first appeared.
	Requests []Request
}

// MainThread is Request.Thread's value for every request that is not a subagent's.
const MainThread = "main"

// Request is one deduplicated request_id's usage, attributed to the thread that spent it. Thread
// is MainThread, or the tool_use id of the Task call whose subagent made the request. Tool is the
// name of the tool the request itself called, empty for a request that called none.
type Request struct {
	ID                  string
	Thread              string
	Tool                string
	InputTokens         int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	OutputTokens        int64
}

// ParseMetrics reads a run's log in the Claude CLI's stream-json dialect into RunMetrics. An
// empty, truncated or absent log is an error, never a zero-valued RunMetrics.
func ParseMetrics(logPath string) (RunMetrics, error) {
	f, err := os.Open(logPath)
	if err != nil {
		return RunMetrics{}, fmt.Errorf("open agent log %s: %w", logPath, err)
	}
	defer func() { _ = f.Close() }()

	var (
		metrics  RunMetrics
		result   *logLine
		decoded  int
		byReqIdx map[string]int
	)

	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return RunMetrics{}, fmt.Errorf("read agent log %s: %w", logPath, readErr)
		}

		parsed, decodeErr := decode(line)
		if decodeErr != nil {
			continue
		}
		decoded++

		if model := parsed.Message.Model; model != "" {
			metrics.Model = model
		}
		countEvent(&metrics, parsed)

		switch parsed.Type {
		case "result":
			resultLine := parsed
			result = &resultLine
		case "assistant":
			tool := firstToolUse(parsed.Message.Content)
			if idx, dup := byReqIdx[parsed.RequestID]; dup {
				if metrics.Requests[idx].Tool == "" {
					metrics.Requests[idx].Tool = tool
				}
				continue
			}
			if byReqIdx == nil {
				byReqIdx = make(map[string]int)
			}
			byReqIdx[parsed.RequestID] = len(metrics.Requests)
			thread := MainThread
			if parsed.ParentToolUseID != "" {
				thread = parsed.ParentToolUseID
			}
			metrics.Requests = append(metrics.Requests, Request{
				ID:                  parsed.RequestID,
				Thread:              thread,
				Tool:                tool,
				InputTokens:         int64(parsed.Message.Usage.Input),
				CacheCreationTokens: int64(parsed.Message.Usage.CacheCreate),
				CacheReadTokens:     int64(parsed.Message.Usage.CacheRead),
				OutputTokens:        int64(parsed.Message.Usage.Output),
			})
			metrics.TokensIn += tokensIn(parsed.Message.Usage)
			metrics.TokensOut += int64(parsed.Message.Usage.Output)
		}
	}

	if decoded == 0 {
		return RunMetrics{}, fmt.Errorf("agent log %s: no parseable lines", logPath)
	}

	if result != nil {
		metrics.Settled = true
		metrics.Turns = result.NumTurns
		metrics.Duration = time.Duration(result.DurationMS) * time.Millisecond
		metrics.CostUSD = result.CostUSD
		metrics.TokensIn = tokensIn(result.Usage)
		metrics.TokensOut = int64(result.Usage.Output)
	}

	return metrics, nil
}

func tokensIn(u usage) int64 {
	return int64(u.Input + u.CacheCreate + u.CacheRead)
}

func firstToolUse(content []contentBlock) string {
	for _, block := range content {
		if block.Type == "tool_use" {
			return block.Name
		}
	}
	return ""
}

func countEvent(metrics *RunMetrics, parsed logLine) {
	event, _, ok := parsed.event()
	if !ok {
		return
	}
	switch event.Kind {
	case Skill, Tool, File:
		metrics.ToolCalls++
	case Fail:
		metrics.ToolFailures++
	case Pass:
	}
}
