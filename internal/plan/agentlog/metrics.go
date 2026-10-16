package agentlog

import (
	"errors"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

var ErrNoParseableLines = errors.New("no parseable lines")

type RunMetrics struct {
	CostUSD  option.Option[float64]
	Requests []RequestUsage
}

// ParseMetrics reads Claude stream-json usage, rejecting logs with no parseable lines.
func ParseMetrics(logPath string) (RunMetrics, error) {
	requests, settled, decoded, err := readUsage(logPath)
	if err != nil {
		return RunMetrics{}, fmt.Errorf("parse run metrics: %w", err)
	}
	if decoded == 0 {
		return RunMetrics{}, fmt.Errorf("agent log %s: %w", logPath, ErrNoParseableLines)
	}
	metrics := RunMetrics{Requests: requests}
	if settled.IsSome() {
		metrics.CostUSD = settled.Unwrap().CostUSD
	}
	return metrics, nil
}
