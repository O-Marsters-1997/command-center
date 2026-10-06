package demo

import (
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

type issue struct {
	Ticket
	repo   *sandboxRepo
	number int
	url    string
	branch string
}

func (i issue) tracker(urlByID map[string]string) tracker.Ticket {
	blockedBy := make([]string, 0, len(i.BlockedBy))
	for _, id := range i.BlockedBy {
		blockedBy = append(blockedBy, urlByID[id])
	}
	return tracker.Ticket{URL: i.url, Number: i.number, Title: i.Title, BlockedBy: blockedBy}
}

func buildIssues(sc Scenario, sb *Sandbox) ([]issue, error) {
	repos := map[string]*sandboxRepo{}
	for _, r := range sb.repos {
		repos[r.scenarioName] = r
	}
	issues := make([]issue, 0, len(sc.Ticket))
	for n, t := range sc.Ticket {
		repo, ok := repos[t.Repo]
		if !ok {
			return nil, fmt.Errorf("ticket %q: no sandbox repo %q", t.ID, t.Repo)
		}
		number := n + 1
		issues = append(issues, issue{
			Ticket: t,
			repo:   repo,
			number: number,
			url:    fmt.Sprintf("https://github.com/%s/issues/%d", t.Repo, number),
			branch: tracker.BranchSlug(number, t.Title),
		})
	}
	return issues, nil
}
