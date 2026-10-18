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
	reader := newRequestReader()
	var (
		model   string
		settled option.Option[RequestUsage]
		metrics RunMetrics
		decoded int
	)
	err := forEachLine(logPath, func(parsed logLine, last time.Time) {
		decoded++
		if parsed.Message.Model != "" {
			model = parsed.Message.Model
		}
		if parsed.Type == "result" {
			request := requestUsage(model, last, parsed.Usage)
			if parsed.CostUSD != nil {
				request.CostUSD = option.Some(*parsed.CostUSD)
			}
			settled = option.Some(request)
			metrics.Wall = option.Some(time.Duration(parsed.DurationMS) * time.Millisecond)
			return
		}
		if _, added := reader.add(parsed); added && parsed.ParentToolUseID == "" {
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
	metrics.Requests = reader.requests
	totals := reader.requests
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
