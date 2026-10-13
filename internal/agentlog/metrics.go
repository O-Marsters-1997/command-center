package agentlog

import (
	"errors"
	"fmt"
	"time"
)

var ErrNoParseableLines = errors.New("no parseable lines")

// RunMetrics is what every dialect must produce. A field a dialect cannot supply is nil or zero
// with Settled false, never a guess.
type RunMetrics struct {
	CostUSD *float64
	Settled bool
	// Requests is one deduplicated request_id per element, in the order each first appeared.
	Requests []Request
}

type Request struct {
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
			spent := parsed.Message.Usage
			metrics.Requests = append(metrics.Requests, Request{
				InputTokens:         int64(spent.Input),
				CacheCreationTokens: int64(spent.CacheCreate),
				CacheReadTokens:     int64(spent.CacheRead),
				OutputTokens:        int64(spent.Output),
			})
		}
	})
	if err != nil {
		return RunMetrics{}, err
	}

	if decoded == 0 {
		return RunMetrics{}, fmt.Errorf("agent log %s: %w", logPath, ErrNoParseableLines)
	}

	if result != nil {
		metrics.Settled = true
		metrics.CostUSD = result.CostUSD
	}

	return metrics, nil
}
