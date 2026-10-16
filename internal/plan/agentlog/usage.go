package agentlog

import (
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

// RequestUsage is a transcript's token spend by category and model.
// CostUSD is present only when the CLI reports a settled run's exact spend.
type RequestUsage struct {
	At                                    time.Time
	Model                                 string
	Input, Output, CacheRead, CacheCreate int64
	CostUSD                               option.Option[float64]
}

type requestReader struct {
	seen     map[string]int
	requests []RequestUsage
}

func newRequestReader() requestReader {
	return requestReader{seen: make(map[string]int)}
}

func (r *requestReader) add(parsed logLine) (int, bool) {
	if parsed.Type != "assistant" || parsed.RequestID == "" {
		return 0, false
	}
	if index, ok := r.seen[parsed.RequestID]; ok {
		return index, false
	}
	index := len(r.requests)
	r.seen[parsed.RequestID] = index
	r.requests = append(r.requests, requestUsage(parsed.Message.Model, parsed.Timestamp, parsed.Message.Usage))
	return index, true
}

func readUsage(logPath string) (requests []RequestUsage, settled option.Option[RequestUsage], decoded int, err error) {
	reader := newRequestReader()
	var model string
	err = forEachLine(logPath, func(parsed logLine, last time.Time) {
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
			return
		}
		reader.add(parsed)
	})
	return reader.requests, settled, decoded, err
}

// ParseTranscriptUsage reads a transcript's settled total or its deduplicated request usage.
func ParseTranscriptUsage(logPath string) ([]RequestUsage, error) {
	requests, settled, _, err := readUsage(logPath)
	if err != nil {
		return nil, fmt.Errorf("parse transcript usage: %w", err)
	}
	if settled.IsSome() {
		return []RequestUsage{settled.Unwrap()}, nil
	}
	return requests, nil
}

func requestUsage(model string, at time.Time, u usage) RequestUsage {
	return RequestUsage{
		At: at, Model: model,
		Input: int64(u.Input), Output: int64(u.Output),
		CacheRead: int64(u.CacheRead), CacheCreate: int64(u.CacheCreate),
	}
}
