package agentlog

import (
	"fmt"
	"time"
)

// RequestUsage is a transcript's token spend by category and model.
// CostUSD is present only when the CLI reports a settled run's exact spend.
type RequestUsage struct {
	At                                    time.Time
	Model                                 string
	Input, Output, CacheRead, CacheCreate int64
	CostUSD                               *float64
}

// ParseTranscriptUsage reads a transcript's settled total or its deduplicated request usage.
func ParseTranscriptUsage(logPath string) ([]RequestUsage, error) {
	var (
		requests []RequestUsage
		settled  *RequestUsage
		model    string
		seen     map[string]bool
	)

	err := forEachLine(logPath, func(parsed logLine, last time.Time) {
		if parsed.Message.Model != "" {
			model = parsed.Message.Model
		}

		switch parsed.Type {
		case "result":
			request := requestUsage(model, last, parsed.Usage)
			request.CostUSD = parsed.CostUSD
			settled = &request
		case "assistant":
			if parsed.RequestID == "" {
				return
			}
			if seen == nil {
				seen = make(map[string]bool)
			}
			if seen[parsed.RequestID] {
				return
			}
			seen[parsed.RequestID] = true
			requests = append(requests, requestUsage(parsed.Message.Model, parsed.Timestamp, parsed.Message.Usage))
		}
	})
	if err != nil {
		return nil, fmt.Errorf("parse transcript usage: %w", err)
	}

	if settled != nil {
		return []RequestUsage{*settled}, nil
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
