package ghtest

import (
	"context"
	"maps"
	"slices"
	"sync"

	"github.com/O-Marsters-1997/command-center/internal/gh"
)

type Call struct {
	Method, Repo, Branch, Base, Body, Issue string
	Draft                                   bool
}

type Forge struct {
	mu       sync.Mutex
	calls    []Call
	Snapshot gh.Snapshot
	Titles   map[string]string
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

func (f *Forge) SetSnapshot(s gh.Snapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Snapshot = s
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
	byBranch := make(map[string]gh.PR, len(f.Snapshot.ByBranch))
	for branch, pr := range f.Snapshot.ByBranch {
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
	titles := maps.Clone(f.Titles)
	if titles == nil {
		titles = map[string]string{}
	}
	return titles, nil
}

func (f *Forge) Create(_ context.Context, repo, base, body string, draft bool) error {
	f.Record(Call{Method: "Create", Repo: repo, Base: base, Body: body, Draft: draft})
	return nil
}

func (f *Forge) Ready(_ context.Context, repo, branch string) error {
	f.Record(Call{Method: "Ready", Repo: repo, Branch: branch})
	return nil
}

func (f *Forge) Edit(_ context.Context, repo, branch, base string) error {
	f.Record(Call{Method: "Edit", Repo: repo, Branch: branch, Base: base})
	return nil
}

func (f *Forge) Comment(_ context.Context, repo, branch, body string) error {
	f.Record(Call{Method: "Comment", Repo: repo, Branch: branch, Body: body})
	return nil
}

func (f *Forge) CloseIssue(_ context.Context, repo, issue string) error {
	f.Record(Call{Method: "CloseIssue", Repo: repo, Issue: issue})
	return nil
}
