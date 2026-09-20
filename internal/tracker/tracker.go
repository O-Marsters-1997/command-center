// Package tracker owns everything about reading an issue tracker, so nothing above it knows
// GitHub exists.
package tracker

import (
	"context"
	"fmt"
	"strings"
)

// Kind names which issue tracker a repo's tickets live in. A config that names none resolves to
// GitHub (internal/cc's LoadConfig applies that default, not this package).
type Kind string

// GitHub is the Kind a [[repo]] with no tracker key resolves to.
const GitHub Kind = "github"

// Feature names one grouping of tickets a Source can list and import as a unit.
type Feature string

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
	// Features lists the repo's fleets currently available to import.
	Features(ctx context.Context) ([]Feature, error)
	// Tickets lists feature's open tickets, whatever their status: blocking order, not status,
	// governs when one can launch.
	Tickets(ctx context.Context, feature string) ([]Ticket, error)
}

// New constructs the Source that reads kind's tracker for the repo named by remote, in the
// host/owner/repo form internal/cc's normaliseRemote produces.
func New(kind Kind, remote string) (Source, error) {
	switch kind {
	case GitHub:
		owner, repo, ok := ownerRepo(remote)
		if !ok {
			return nil, fmt.Errorf("tracker: cannot read owner/repo from %q", remote)
		}
		return newGithubSource(owner, repo), nil
	default:
		return nil, fmt.Errorf("tracker: no source for kind %q", kind)
	}
}

func ownerRepo(remote string) (owner, repo string, ok bool) {
	segments := strings.Split(strings.Trim(remote, "/"), "/")
	if len(segments) < 2 {
		return "", "", false
	}
	owner, repo = segments[len(segments)-2], segments[len(segments)-1]
	if owner == "" || repo == "" {
		return "", "", false
	}
	return owner, repo, true
}
