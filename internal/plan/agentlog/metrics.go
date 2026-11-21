package agentlog

import (
	"errors"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

var ErrNoParseableLines = errors.New("no parseable lines")

type RunMetrics struct {
	CostUSD    option.Option[float64]       `json:"-"`
	Requests   []RequestUsage               `json:"-"`
	Wall       option.Option[time.Duration] `json:"wall"`
	Turns      int                          `json:"turns"`
	Input      int64                        `json:"input"`
	CacheWrite int64                        `json:"cache_write"`
	CacheRead  int64                        `json:"cache_read"`
	Output     int64                        `json:"output"`
	Tools      map[string]int               `json:"tools"`
}

func ParseLastMessage(logPath string) (option.Option[string], error) {
	last := option.None[string]()
	err := forEachLine(logPath, func(parsed logLine, _ time.Time) {
		if parsed.Type == "result" && parsed.ResultText != "" {
			last = option.Some(parsed.ResultText)
		}
	})
	if err != nil {
		return option.None[string](), fmt.Errorf("parse last message: %w", err)
	}
	return last, nil
}

// ParseMetrics reads Claude stream-json usage, rejecting logs with no parseable lines.
func ParseMetrics(logPath string) (RunMetrics, error) {
	var metrics RunMetrics
	requests, settled, decoded, err := readUsage(logPath, func(parsed logLine, added bool) {
		if parsed.Type == "result" {
			metrics.Wall = option.Some(time.Duration(parsed.DurationMS) * time.Millisecond)
			return
		}
		if added && parsed.ParentToolUseID == "" {
			metrics.Turns++
		}
		if parsed.Type != "assistant" {
			return
		}
		for _, block := range parsed.Message.Content {
			if block.Type != "tool_use" {
				continue
			}
			if metrics.Tools == nil {
				metrics.Tools = make(map[string]int)
			}
			metrics.Tools[block.Name]++
		}
	})
	if err != nil {
		return RunMetrics{}, fmt.Errorf("parse run metrics: %w", err)
	}
	if decoded == 0 {
		return RunMetrics{}, fmt.Errorf("agent log %s: %w", logPath, ErrNoParseableLines)
	}
	metrics.Requests = requests
	totals := requests
	if settled.IsSome() {
		metrics.CostUSD = settled.Unwrap().CostUSD
		totals = []RequestUsage{settled.Unwrap()}
	}
	for _, r := range totals {
		metrics.Input += r.Input
		metrics.CacheWrite += r.CacheCreate
		metrics.CacheRead += r.CacheRead
		metrics.Output += r.Output
	}
	return metrics, nil
}
