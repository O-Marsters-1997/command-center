package demo

import (
	"context"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

type fakeSource struct {
	issues  []issue
	urlByID map[string]string
}

func (f fakeSource) Features(context.Context) ([]tracker.Feature, error) {
	var out []tracker.Feature
	for _, feature := range features(f.issues) {
		out = append(out, tracker.Feature(feature))
	}
	return out, nil
}

func (f fakeSource) Tickets(_ context.Context, feature string) ([]tracker.Ticket, error) {
	var tickets []tracker.Ticket
	for _, i := range f.issues {
		if i.Feature == feature {
			tickets = append(tickets, i.tracker(f.urlByID))
		}
	}
	return tickets, nil
}

func trackerSource(issues []issue) tracker.Resolver {
	urlByID := make(map[string]string, len(issues))
	for _, i := range issues {
		urlByID[i.ID] = i.url
	}
	return func(_ tracker.Kind, remote string) (tracker.Source, error) {
		var mine []issue
		for _, i := range issues {
			if strings.EqualFold(strings.TrimSuffix(i.repo.origin, ".git"), remote) {
				mine = append(mine, i)
			}
		}
		return fakeSource{issues: mine, urlByID: urlByID}, nil
	}
}
