package cc

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// importVerb is the intent verb POST /import queues and the loop's applyImportIntents consumes.
// It is not in supportedVerbs (verbs.go): that map is for verbs against an existing ticket, and
// an import intent's own "ticket" id is a group label, not a url.
const importVerb = "import"

// TrackerSource resolves the tracker.Source that reads ticketURL's issue tracker. tracker.For in
// production; a test substitutes a fake here rather than shelling out to gh.
type TrackerSource func(ticketURL string) (tracker.Source, error)

// ImportedTicket is one tracker.Ticket paired with the configured repo its url resolves to --
// Store.ImportTickets's own upsert unit.
type ImportedTicket struct {
	tracker.Ticket
	Repo string
}

// ImportGroup is one project: label available to import, plus the tickets it would currently
// bring in.
type ImportGroup struct {
	Group   string
	Tickets []tracker.Ticket
}

// ImportGroups reads every configured repo's tracker groups and, for each, the tickets it would
// bring in right now -- GET /import's whole view, gathered fresh on every render (inv. 14). A
// repo with no remote has no tracker to dispatch to and is silently skipped.
func ImportGroups(ctx context.Context, repos []Repo, resolve TrackerSource) ([]ImportGroup, error) {
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

		groups, err := src.Groups(ctx)
		if err != nil {
			return nil, fmt.Errorf("list groups for %s: %w", r.Name, err)
		}
		for _, g := range groups {
			if !seen[string(g)] {
				seen[string(g)] = true
				names = append(names, string(g))
			}
		}
	}
	sort.Strings(names)

	result := make([]ImportGroup, 0, len(names))
	for _, name := range names {
		var tickets []tracker.Ticket
		for _, src := range sources {
			ts, err := src.Tickets(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("list tickets for %s: %w", name, err)
			}
			tickets = append(tickets, ts...)
		}
		result = append(result, ImportGroup{Group: name, Tickets: tickets})
	}
	return result, nil
}

// trackerSourceFor builds the tracker.Source a remote-carrying repo reads from, treating its own
// remote as the url tracker.For dispatches on. A path-only repo has no remote to dispatch with,
// so it answers ok=false rather than an error: it simply has no groups to offer.
func trackerSourceFor(r Repo, resolve TrackerSource) (tracker.Source, bool, error) {
	if r.Remote == "" {
		return nil, false, nil
	}
	src, err := resolve("https://" + normaliseRemote(r.Remote))
	if err != nil {
		return nil, false, err
	}
	return src, true, nil
}

// repoForTicketURL matches ticketURL's own owner and repo against every configured repo's
// remote, the same normalised form EnsureCheckout compares origins with (checkout.go), so the
// ssh and https forms of one repository resolve to the same configured name.
func repoForTicketURL(ticketURL string, repos []Repo) (string, bool) {
	u, err := url.Parse(ticketURL)
	if err != nil {
		return "", false
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) < 2 {
		return "", false
	}
	target := strings.ToLower(u.Host + "/" + segments[0] + "/" + segments[1])
	for _, r := range repos {
		if r.Remote != "" && normaliseRemote(r.Remote) == target {
			return r.Name, true
		}
	}
	return "", false
}
