package agentlog

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Window is one of the two account-wide rate limit windows a rate_limit_event line reports.
type Window string

const (
	FiveHour Window = "five_hour"
	SevenDay Window = "seven_day"
)

// Reading is one rate-limit window's utilization.
// The CLI's rate_limit_event has no timestamp, so At uses the preceding line's time.
type Reading struct {
	Window      Window
	Utilization float64
	ResetsAt    time.Time
	At          time.Time
}

// ParseReadings returns one Reading per rate-limit window reported in a log.
func ParseReadings(logPath string) ([]Reading, error) {
	var readings []Reading
	err := forEachLine(logPath, func(parsed logLine, last time.Time) {
		if parsed.RateLimitInfo == nil {
			return
		}
		for window, w := range parsed.RateLimitInfo.UnifiedWindows {
			readings = append(readings, Reading{
				Window: Window(window), Utilization: w.Utilization,
				ResetsAt: time.Unix(w.ResetsAt, 0).UTC(), At: last,
			})
		}
	})
	if err != nil {
		return readings, fmt.Errorf("parse rate limit readings: %w", err)
	}

	slices.SortFunc(readings, func(a, b Reading) int {
		if a.Window != b.Window {
			return strings.Compare(string(a.Window), string(b.Window))
		}
		return a.At.Compare(b.At)
	})
	return readings, nil
}
