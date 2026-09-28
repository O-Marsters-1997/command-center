package agentlog

import "time"

// RequestUsage is one transcript's token spend, split by pricing category and tagged with the
// model and the time it happened, so Weight can price it and a caller can bucket it by interval.
// CostUSD is set only for a settled run, whose result line already carries its exact spend.
type RequestUsage struct {
	At                                    time.Time
	Model                                 string
	Input, Output, CacheRead, CacheCreate int64
	CostUSD                               *float64
}

// ParseTranscriptUsage reads a transcript's token spend. A settled run (one ending in a result
// line) reports that line's own cumulative usage as a single entry; an interactive session, which
// never emits one, falls back to summing every request instead, deduped by request_id.
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
		return nil, err
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
