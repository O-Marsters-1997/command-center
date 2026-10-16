package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/store"
)

func applyActivities(rows []Row, activities []store.Activity, now time.Time) {
	byTicket := make(map[string]store.Activity, len(activities))
	for _, a := range activities {
		byTicket[a.TicketURL] = a
	}
	for i := range rows {
		a, ok := byTicket[rows[i].URL]
		if !ok {
			continue
		}
		rows[i].Updated = Age{Age: shortAge(now.Sub(a.At)), Stamp: a.At.UTC().Format(time.RFC3339)}
		rows[i].Last = firstLine(a.LastMessage)
	}
}

func shortAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
