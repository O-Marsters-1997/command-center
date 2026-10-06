package demo

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
)

const ciCheck = "CI"

type pullRequest struct {
	issue    issue
	number   int
	draft    bool
	state    gh.PRState
	baseRef  string
	openedAt time.Time
	mergedAt time.Time
}

// Forge is the in-memory GitHub. It keeps pull requests and plays the scenario's CI and merge
// steps against real git origins.
type Forge struct {
	clock  cc.Clock
	sb     *Sandbox
	issues []issue

	mu   sync.Mutex
	prs  map[string]*pullRequest
	next int
}

// NewForge returns a forge holding no pull requests.
func NewForge(clock cc.Clock, sb *Sandbox, issues []issue) *Forge {
	return &Forge{clock: clock, sb: sb, issues: issues, prs: map[string]*pullRequest{}}
}

func prKey(repo *sandboxRepo, branch string) string { return repo.origin + "\x00" + branch }

func (f *Forge) issueFor(repo *sandboxRepo, branch string) (issue, bool) {
	for _, i := range f.issues {
		if i.repo == repo && i.branch == branch {
			return i, true
		}
	}
	return issue{}, false
}

// Advance merges every open, non-draft pull request whose scripted merge time has come.
func (f *Forge) Advance() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.clock.Now()
	prs := slices.SortedFunc(maps.Values(f.prs), func(a, b *pullRequest) int { return cmp.Compare(a.number, b.number) })
	for _, pr := range prs {
		due := pr.openedAt.Add(time.Duration(pr.issue.Merge.After))
		if pr.state != gh.Open || pr.draft || now.Before(due) {
			continue
		}
		if err := pr.issue.repo.squashMerge(pr.issue.branch, pr.issue.Title); err != nil {
			return err
		}
		pr.state = gh.Merged
		pr.mergedAt = now
	}
	return nil
}

func (f *Forge) List(_ context.Context, repoPath string, tracked []string) (gh.Snapshot, error) {
	repo, err := f.sb.repoFor(repoPath)
	if err != nil {
		return gh.Snapshot{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	byBranch := make(map[string]gh.PR, len(tracked))
	for _, branch := range tracked {
		pr, ok := f.prs[prKey(repo, branch)]
		if !ok {
			continue
		}
		headOid, err := git(repo.origin, "rev-parse", "refs/heads/"+branch)
		if err != nil {
			return gh.Snapshot{}, err
		}
		byBranch[branch] = gh.PR{
			Number: pr.number, HeadRef: branch, HeadOid: headOid, BaseRef: pr.baseRef,
			IsDraft: pr.draft, State: pr.state, MergedAt: pr.mergedAt,
			Checks: map[string]gh.CheckState{ciCheck: {
				Name: ciCheck, Status: "COMPLETED", Conclusion: "SUCCESS", StartedAt: pr.openedAt,
			}},
		}
	}
	return gh.Snapshot{ByBranch: byBranch}, nil
}

func (f *Forge) IssueTitles(_ context.Context, repoPath string) (map[string]string, error) {
	repo, err := f.sb.repoFor(repoPath)
	if err != nil {
		return nil, err
	}
	titles := map[string]string{}
	for _, i := range f.issues {
		if i.repo == repo {
			titles[i.url] = i.Title
		}
	}
	return titles, nil
}

// Create opens a pull request for the branch checked out at worktreePath.
func (f *Forge) Create(_ context.Context, worktreePath, base, _ string, draft bool) error {
	repo, err := f.sb.repoFor(worktreePath)
	if err != nil {
		return err
	}
	branch, err := git(worktreePath, "branch", "--show-current")
	if err != nil {
		return err
	}
	is, ok := f.issueFor(repo, branch)
	if !ok {
		return fmt.Errorf("no scenario ticket owns branch %s", branch)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	f.prs[prKey(repo, branch)] = &pullRequest{
		issue: is, number: f.next, draft: draft, state: gh.Open, baseRef: base, openedAt: f.clock.Now(),
	}
	return nil
}

func (f *Forge) Ready(_ context.Context, repoPath, branch string) error {
	return f.update(repoPath, branch, func(pr *pullRequest) { pr.draft = false })
}

func (f *Forge) Edit(_ context.Context, repoPath, branch, base string) error {
	return f.update(repoPath, branch, func(pr *pullRequest) { pr.baseRef = base })
}

func (f *Forge) Close(_ context.Context, repoPath, branch string) error {
	return f.update(repoPath, branch, func(pr *pullRequest) { pr.state = gh.Closed })
}

func (f *Forge) update(repoPath, branch string, change func(*pullRequest)) error {
	repo, err := f.sb.repoFor(repoPath)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	pr, ok := f.prs[prKey(repo, branch)]
	if !ok {
		return fmt.Errorf("no pull request for %s", branch)
	}
	change(pr)
	return nil
}

func (*Forge) Rerun(context.Context, string, string) error { return nil }

func (*Forge) RunViewLogFailed(context.Context, string, string) (string, error) {
	return "", nil
}

func (*Forge) CloseIssue(context.Context, string, string) error { return nil }

var _ gh.Forge = (*Forge)(nil)
