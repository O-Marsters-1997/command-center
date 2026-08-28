// Package tracker owns everything about reading an issue tracker, so nothing above it knows
// GitHub exists.
package tracker

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Group is a project:-prefixed label naming one fleet.
type Group string

// Ticket is one in-flight issue as the tracker reports it. It is deliberately not cc.Ticket:
// this package never learns the app's columns.
type Ticket struct {
	URL       string
	Number    int
	Title     string
	Body      string
	Status    string
	BlockedBy []string
}

// Source reads one repo's tracker.
type Source interface {
	// Groups lists the repo's project:-prefixed labels.
	Groups(ctx context.Context) ([]Group, error)
	// Tickets lists group's open issues carrying status:ready or beyond.
	Tickets(ctx context.Context, group string) ([]Ticket, error)
}

// For dispatches on ticketURL's host and returns the Source that reads it.
func For(ticketURL string) (Source, error) {
	u, err := url.Parse(ticketURL)
	if err != nil {
		return nil, fmt.Errorf("tracker: parse %q: %w", ticketURL, err)
	}

	switch u.Host {
	case "github.com":
		owner, repo, ok := ownerRepo(u.Path)
		if !ok {
			return nil, fmt.Errorf("tracker: cannot read owner/repo from %q", ticketURL)
		}
		return newGithubSource(owner, repo), nil
	default:
		return nil, fmt.Errorf("tracker: no source for host %q", u.Host)
	}
}

func ownerRepo(path string) (owner, repo string, ok bool) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return "", "", false
	}
	return segments[0], segments[1], true
}
