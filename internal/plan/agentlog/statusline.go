package agentlog

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type statusline struct {
	RateLimits map[Window]struct {
		UsedPercentage *float64 `json:"used_percentage"`
		ResetsAt       int64    `json:"resets_at"`
	} `json:"rate_limits"`
}

func ParseStatusline(r io.Reader, at time.Time) ([]Reading, error) {
	var payload statusline
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode statusline: %w", err)
	}
	var readings []Reading
	for _, window := range []Window{FiveHour, SevenDay} {
		w, ok := payload.RateLimits[window]
		if !ok || w.UsedPercentage == nil || w.ResetsAt == 0 {
			continue
		}
		readings = append(readings, Reading{
			Window: window, Utilization: *w.UsedPercentage / 100,
			ResetsAt: time.Unix(w.ResetsAt, 0).UTC(), At: at.UTC(),
		})
	}
	return readings, nil
}
