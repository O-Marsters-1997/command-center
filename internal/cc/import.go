package cc

import (
	"context"
	"fmt"
	"sort"

	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// importVerb is the intent verb POST /import queues and the loop's applyImportIntents consumes.
// It is not in supportedVerbs (verbs.go): that map is for verbs against an existing ticket, and
// an import intent's own "ticket" id is a feature label, not a url.
const importVerb = "import"

// TrackerSource resolves the tracker.Source that reads one repo's tracker. tracker.New in
// production; a test substitutes a fake here rather than shelling out to gh.
type TrackerSource func(kind tracker.Kind, remote string) (tracker.Source, error)

// ImportedTicket is one tracker.Ticket paired with the configured repo it came from and that
// repo's tracker kind -- Store.ImportTickets's own upsert unit.
type ImportedTicket struct {
	tracker.Ticket
	Repo   string
	Source string
}

// ImportFeature is one project: label available to import, plus the tickets it would currently
// bring in.
type ImportFeature struct {
	Feature string
	Tickets []tracker.Ticket
}

// ImportFeatures reads every configured repo's tracker features and, for each, the tickets it
// would bring in right now -- GET /import's whole view, gathered fresh on every render (inv. 14).
// A repo with no remote has no tracker to dispatch to and is silently skipped.
func ImportFeatures(ctx context.Context, repos []Repo, resolve TrackerSource) ([]ImportFeature, error) {
	var names []string
	seen := map[string]bool{}
	var sources []tracker.Source
	for _, r := range repos {
		src, ok, err := trackerSourceFor(r, resolve)
		if err != nil {
			return nil, fmt.Errorf("repo %s: %w", r.Name, err)
		}
		if !ok {
			continue
		}
		sources = append(sources, src)

		features, err := src.Features(ctx)
		if err != nil {
			return nil, fmt.Errorf("list features for %s: %w", r.Name, err)
		}
		for _, f := range features {
			if !seen[string(f)] {
				seen[string(f)] = true
				names = append(names, string(f))
			}
		}
	}
	sort.Strings(names)

	result := make([]ImportFeature, 0, len(names))
	for _, name := range names {
		var tickets []tracker.Ticket
		for _, src := range sources {
			ts, err := src.Tickets(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("list tickets for %s: %w", name, err)
			}
			tickets = append(tickets, ts...)
		}
		result = append(result, ImportFeature{Feature: name, Tickets: tickets})
	}
	return result, nil
}

func trackerSourceFor(r Repo, resolve TrackerSource) (tracker.Source, bool, error) {
	if r.Remote == "" {
		return nil, false, nil
	}
	src, err := resolve(tracker.Kind(r.Tracker), normaliseRemote(r.Remote))
	if err != nil {
		return nil, false, err
	}
	return src, true, nil
}
