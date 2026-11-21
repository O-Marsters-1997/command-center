package ghtest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
)

type Call struct {
	Method, Repo, Branch, Base, Body, Issue, URL string
	Draft                                        bool
	Labels                                       []string
}

type Forge struct {
	mu        sync.Mutex
	calls     []Call
	prs       map[string]gh.PR
	titles    map[string]string
	logs      map[string]string
	attempts  map[string]int
	checkouts map[string]string
}

var _ gh.Forge = (*Forge)(nil)

func (f *Forge) Calls(method string) []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var calls []Call
	for _, call := range f.calls {
		if call.Method == method {
			calls = append(calls, call)
		}
	}
	return calls
}

func (f *Forge) Seed(seed Seed) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prs = make(map[string]gh.PR, len(seed.PRs))
	for _, pr := range seed.PRs {
		f.prs[pr.HeadRef] = pr
	}
	f.titles = maps.Clone(seed.Titles)
	f.logs = maps.Clone(seed.Logs)
	f.checkouts = maps.Clone(seed.Checkouts)
}

func (f *Forge) Record(call Call) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *Forge) List(_ context.Context, repo string, tracked []string) (gh.Snapshot, error) {
	f.Record(Call{Method: "List", Repo: repo})
	f.mu.Lock()
	defer f.mu.Unlock()
	byBranch := make(map[string]gh.PR, len(f.prs))
	for branch, pr := range f.prs {
		if pr.State == gh.Open || slices.Contains(tracked, branch) {
			byBranch[branch] = pr
		}
	}
	return gh.Snapshot{ByBranch: byBranch}, nil
}

func (f *Forge) IssueTitles(_ context.Context, repo string) (map[string]string, error) {
	f.Record(Call{Method: "IssueTitles", Repo: repo})
	f.mu.Lock()
	defer f.mu.Unlock()
	titles := maps.Clone(f.titles)
	if titles == nil {
		titles = map[string]string{}
	}
	return titles, nil
}

func (f *Forge) FailedLog(_ context.Context, repo, detailsURL string) (string, error) {
	f.Record(Call{Method: "FailedLog", Repo: repo, URL: detailsURL})
	if _, err := gh.RunID(detailsURL); err != nil {
		return "", fmt.Errorf("read failed log: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	log, ok := f.logs[detailsURL]
	if !ok {
		return "", fmt.Errorf("no failed log for %s", detailsURL)
	}
	return log, nil
}

func (f *Forge) Attempt(_ context.Context, repo, detailsURL string) (int, error) {
	f.Record(Call{Method: "Attempt", Repo: repo, URL: detailsURL})
	id, err := gh.RunID(detailsURL)
	if err != nil {
		return 0, fmt.Errorf("read attempt: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return max(f.attempts[id], 1), nil
}

func (f *Forge) Rerun(_ context.Context, repo, detailsURL string) error {
	f.Record(Call{Method: "Rerun", Repo: repo, URL: detailsURL})
	id, err := gh.RunID(detailsURL)
	if err != nil {
		return fmt.Errorf("rerun: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.attempts == nil {
		f.attempts = map[string]int{}
	}
	f.attempts[id] = max(f.attempts[id], 1) + 1
	return nil
}

func (f *Forge) Create(_ context.Context, repo, base, body string, draft bool, labels []string) error {
	f.Record(Call{Method: "Create", Repo: repo, Base: base, Body: body, Draft: draft, Labels: labels})
	f.mu.Lock()
	defer f.mu.Unlock()
	branch, ok := f.checkouts[repo]
	if !ok {
		return fmt.Errorf("create pull request: no seeded checkout at %s", repo)
	}
	number := 1
	for _, pr := range f.prs {
		number = max(number, pr.Number+1)
	}
	if f.prs == nil {
		f.prs = map[string]gh.PR{}
	}
	f.prs[branch] = gh.PR{
		Number: number, HeadRef: branch, BaseRef: base, IsDraft: draft, State: gh.Open,
		Labels: append([]string{"keep-open"}, labels...),
	}
	return nil
}

func (f *Forge) Ready(_ context.Context, repo, branch string) error {
	f.Record(Call{Method: "Ready", Repo: repo, Branch: branch})
	return f.editPR(branch, func(pr *gh.PR) { pr.IsDraft = false })
}

func (f *Forge) Edit(_ context.Context, repo, branch, base string) error {
	f.Record(Call{Method: "Edit", Repo: repo, Branch: branch, Base: base})
	return f.editPR(branch, func(pr *gh.PR) { pr.BaseRef = base })
}

func (f *Forge) editPR(branch string, change func(*gh.PR)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	pr, ok := f.prs[branch]
	if !ok {
		return fmt.Errorf("no pull request for %s", branch)
	}
	change(&pr)
	f.prs[branch] = pr
	return nil
}

func (f *Forge) Comment(_ context.Context, repo, branch, body string) error {
	f.Record(Call{Method: "Comment", Repo: repo, Branch: branch, Body: body})
	return nil
}

func (f *Forge) CloseIssue(_ context.Context, repo, issue string) error {
	f.Record(Call{Method: "CloseIssue", Repo: repo, Issue: issue})
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.titles, issue)
	return nil
}
