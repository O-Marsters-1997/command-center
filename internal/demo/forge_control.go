package demo

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/gh"
)

type PRSummary struct {
	Number int
	Ticket string
	Title  string
}

func (f *Forge) OpenPRs() []PRSummary {
	f.mu.Lock()
	defer f.mu.Unlock()
	prs := slices.SortedFunc(maps.Values(f.prs), func(a, b *pullRequest) int { return cmp.Compare(a.number, b.number) })
	var out []PRSummary
	for _, pr := range prs {
		if pr.state == gh.Open && !pr.draft {
			out = append(out, PRSummary{Number: pr.number, Ticket: pr.issue.ID, Title: pr.issue.Title})
		}
	}
	return out
}

func (f *Forge) MergeNow(number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, pr := range f.prs {
		if pr.number != number {
			continue
		}
		if pr.state != gh.Open || pr.draft {
			return fmt.Errorf("pull request #%d is not open and ready", number)
		}
		pr.issue.Merge.After = 0
		return nil
	}
	return fmt.Errorf("no pull request #%d", number)
}
