package agentlog

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

type statusline struct {
	RateLimits map[Window]struct {
		UsedPercentage option.Option[float64] `json:"used_percentage"`
		ResetsAt       int64                  `json:"resets_at"`
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
		if !ok || w.UsedPercentage.IsNone() || w.ResetsAt == 0 {
			continue
		}
		readings = append(readings, Reading{
			Window: window, Utilization: w.UsedPercentage.Unwrap() / 100,
			ResetsAt: time.Unix(w.ResetsAt, 0).UTC(), At: at.UTC(),
		})
	}
	return readings, nil
}
