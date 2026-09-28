package agentlog

import (
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

// Reading is one window's utilization off a rate_limit_event line. At is the nearest preceding
// line's own timestamp, since the event itself carries none -- which is what lets two logs that
// replay the same transcript up to the same point agree on when it happened, and so store it once.
type Reading struct {
	Window      Window
	Utilization float64
	ResetsAt    time.Time
	At          time.Time
}

// ParseReadings reads a run's log for every rate_limit_event line, returning one Reading per
// window it reports. A log with no such line returns no readings and no error.
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
		return readings, err
	}

	slices.SortFunc(readings, func(a, b Reading) int {
		if a.Window != b.Window {
			return strings.Compare(string(a.Window), string(b.Window))
		}
		return a.At.Compare(b.At)
	})
	return readings, nil
}
