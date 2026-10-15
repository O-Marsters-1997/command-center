package agentlog

import (
	"errors"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/option"
)

var ErrNoParseableLines = errors.New("no parseable lines")

type RunMetrics struct {
	CostUSD  option.Option[float64]
	Requests []Request
}

type Request struct {
	InputTokens         int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	OutputTokens        int64
}

// ParseMetrics reads Claude stream-json usage, rejecting logs with no parseable lines.
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
		return RunMetrics{}, fmt.Errorf("parse run metrics: %w", err)
	}

	if decoded == 0 {
		return RunMetrics{}, fmt.Errorf("agent log %s: %w", logPath, ErrNoParseableLines)
	}

	if result != nil {
		if result.CostUSD != nil {
			metrics.CostUSD = option.Some(*result.CostUSD)
		}
	}

	return metrics, nil
}
