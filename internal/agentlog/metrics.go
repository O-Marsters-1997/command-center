package agentlog

import (
	"fmt"
	"time"
)

// RunMetrics is what every dialect must produce. A field a dialect cannot supply is nil or zero
// with Settled false, never a guess.
type RunMetrics struct {
	TokensIn  int64
	TokensOut int64
	Turns     int
	Duration  time.Duration
	CostUSD   *float64
	Model     string
	Settled   bool
	// Requests is one deduplicated request_id per element, in the order each first appeared.
	Requests []Request
}

// MainThread is Request.Thread's value for every request that is not a subagent's.
const MainThread = "main"

// Request is one deduplicated request_id's usage, attributed to the thread that spent it. Thread
// is MainThread, or the tool_use id of the Task call whose subagent made the request.
type Request struct {
	ID                  string
	Thread              string
	InputTokens         int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	OutputTokens        int64
}

// ParseMetrics reads a run's log in the Claude CLI's stream-json dialect into RunMetrics. An
// empty, truncated or absent log is an error, never a zero-valued RunMetrics.
func ParseMetrics(logPath string) (RunMetrics, error) {
	var (
		metrics RunMetrics
		result  *logLine
		decoded int
		seen    map[string]struct{}
	)

	err := forEachLine(logPath, func(parsed logLine, _ time.Time) {
		decoded++

		if model := parsed.Message.Model; model != "" {
			metrics.Model = model
		}

		switch parsed.Type {
		case "result":
			result = &parsed
		case "assistant":
			if _, dup := seen[parsed.RequestID]; dup {
				return
			}
			if seen == nil {
				seen = make(map[string]struct{})
			}
			seen[parsed.RequestID] = struct{}{}
			thread := MainThread
			if parsed.ParentToolUseID != "" {
				thread = parsed.ParentToolUseID
			}
			spent := parsed.Message.Usage
			metrics.Requests = append(metrics.Requests, Request{
				ID:                  parsed.RequestID,
				Thread:              thread,
				InputTokens:         int64(spent.Input),
				CacheCreationTokens: int64(spent.CacheCreate),
				CacheReadTokens:     int64(spent.CacheRead),
				OutputTokens:        int64(spent.Output),
			})
			metrics.TokensIn += tokensIn(spent)
			metrics.TokensOut += int64(spent.Output)
		}
	})
	if err != nil {
		return RunMetrics{}, err
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
