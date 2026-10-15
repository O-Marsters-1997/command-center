package trackertest

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

type Source struct {
	Features []tracker.Feature
	Tickets  map[string][]tracker.Ticket
}

type sourceAdapter struct{ *Source }

func (s sourceAdapter) Features(context.Context) ([]tracker.Feature, error) {
	return s.Source.Features, nil
}

func (s sourceAdapter) Tickets(_ context.Context, feature string) ([]tracker.Ticket, error) {
	return s.Source.Tickets[feature], nil
}

func ByRemote(sources map[string]*Source) tracker.Resolver {
	return func(_ tracker.Kind, remote string) (tracker.Source, error) {
		src, ok := sources[remote]
		if !ok {
			return nil, fmt.Errorf("no source for %q", remote)
		}
		return sourceAdapter{src}, nil
	}
}
