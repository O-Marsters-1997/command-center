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
		metrics RunMetrics
		result  *logLine
		decoded int
		seen    map[string]struct{}
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
			if seen == nil {
				seen = make(map[string]struct{})
			}
			if _, dup := seen[parsed.RequestID]; dup {
				continue
			}
			seen[parsed.RequestID] = struct{}{}
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
