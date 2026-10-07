// Package tracker owns everything about reading an issue tracker, so nothing above it knows
// GitHub exists.
package tracker

import (
	"context"
	"fmt"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/git"
)

type Kind string

const GitHub Kind = "github"

type Feature string

// Ticket is one in-flight issue as the tracker reports it. It is deliberately not
// loop.Ticket: this package never learns the app's columns.
type Ticket struct {
	URL       string
	Number    int
	Title     string
	Body      string
	Status    string
	BlockedBy []string
}

type Source interface {
	Features(ctx context.Context) ([]Feature, error)
	Tickets(ctx context.Context, feature string) ([]Ticket, error)
}

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

type Resolver func(kind Kind, remote string) (Source, error)

// ForRemote resolves the Source for a repo's configured remote. A repo with no remote has no
// tracker to read, which is ok=false rather than an error.
func ForRemote(resolve Resolver, kind Kind, remote string) (src Source, ok bool, err error) {
	if remote == "" {
		return nil, false, nil
	}
	src, err = resolve(kind, git.NormaliseRemote(remote))
	if err != nil {
		return nil, false, err
	}
	return src, true, nil
}
