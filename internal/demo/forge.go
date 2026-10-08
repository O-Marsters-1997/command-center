package demo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/loop"
)

const ciCheck = "CI"

const (
	ciPass = "pass"
	ciFail = "fail"
	ciHang = "hang"
)

type pullRequest struct {
	issue    issue
	number   int
	draft    bool
	state    gh.PRState
	baseRef  string
	openedAt time.Time
	mergedAt time.Time

	headOid  string
	runs     int
	runStart time.Time
	collided bool
}

func (pr *pullRequest) ciOutcome() string {
	seq := pr.issue.CI
	if len(seq) == 0 {
		return ciPass
	}
	return seq[min(pr.runs, len(seq))-1]
}

func (pr *pullRequest) startRun(now time.Time) {
	pr.runs++
	pr.runStart = now
}

func (pr *pullRequest) running(now time.Time) bool {
	return pr.ciOutcome() == ciHang || now.Before(pr.runStart.Add(pr.issue.CIAfter))
}

func (pr *pullRequest) compatOutcome() string {
	if pr.issue.CompatFails && pr.runs < 2 {
		return ciFail
	}
	return ciPass
}

func (pr *pullRequest) greenAt(now time.Time) bool {
	return !pr.running(now) && pr.ciOutcome() == ciPass && pr.compatOutcome() == ciPass
}

func (pr *pullRequest) checks(now time.Time, repo *sandboxRepo) map[string]gh.CheckState {
	checks := map[string]gh.CheckState{ciCheck: pr.check(now, repo.scenarioName, ciCheck, pr.ciOutcome())}
	if repo.compatCheck != "" {
		checks[repo.compatCheck] = pr.check(now, repo.scenarioName, repo.compatCheck, pr.compatOutcome())
	}
	return checks
}

func (pr *pullRequest) check(now time.Time, repo, name, outcome string) gh.CheckState {
	state := gh.CheckState{
		Name: name, StartedAt: pr.runStart,
		DetailsURL: fmt.Sprintf("https://github.com/%s/actions/runs/%d/job/1", repo, pr.number),
	}
	switch {
	case pr.running(now):
		state.Status = "IN_PROGRESS"
	case outcome == ciPass:
		state.Status, state.Conclusion = "COMPLETED", "SUCCESS"
	default:
		state.Status, state.Conclusion = "COMPLETED", "FAILURE"
	}
	return state
}

// Forge is the in-memory GitHub. It keeps pull requests and plays the scenario's CI and merge
// steps against real git origins.
type Forge struct {
	clock  loop.Clock
	sb     *Sandbox
	issues []issue

	mu   sync.Mutex
	prs  map[string]*pullRequest
	next int
}

func NewForge(clock loop.Clock, sb *Sandbox, issues []issue) *Forge {
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

// Advance lands a main commit on a conflict ticket's file once its pull request has been open
// half its merge time, and merges every open, non-draft pull request whose merge time has come,
// whose checks are green and that still merges cleanly.
func (f *Forge) Advance() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.clock.Now()
	prs := slices.SortedFunc(maps.Values(f.prs), func(a, b *pullRequest) int { return cmp.Compare(a.number, b.number) })
	for _, pr := range prs {
		if pr.state != gh.Open {
			continue
		}
		if pr.issue.Result == resultConflict && !pr.collided && !now.Before(pr.openedAt.Add(pr.issue.MergeAfter/2)) {
			pr.collided = true
			if err := f.collide(pr); err != nil {
				return err
			}
			continue
		}
		if pr.draft || now.Before(pr.openedAt.Add(pr.issue.MergeAfter)) || !pr.greenAt(now) {
			continue
		}
		err := pr.issue.repo.squashMerge(pr.issue.branch, pr.issue.Title)
		if errors.Is(err, errMergeConflict) {
			continue
		}
		if err != nil {
			return err
		}
		pr.state = gh.Merged
		pr.mergedAt = now
	}
	return nil
}

func (f *Forge) collide(pr *pullRequest) error {
	files := make(map[string]string, len(pr.issue.Files))
	for _, name := range pr.issue.Files {
		files[name] = "package main // landed on main\n"
	}
	return f.sb.LandOnMain(pr.issue.Repo, files)
}

func (f *Forge) List(_ context.Context, repoPath string, tracked []string) (gh.Snapshot, error) {
	repo, err := f.sb.repoFor(repoPath)
	if err != nil {
		return gh.Snapshot{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.clock.Now()
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
		if pr.headOid != "" && pr.headOid != headOid {
			pr.startRun(now)
		}
		pr.headOid = headOid
		byBranch[branch] = gh.PR{
			Number: pr.number, HeadRef: branch, HeadOid: headOid, BaseRef: pr.baseRef,
			IsDraft: pr.draft, State: pr.state, MergedAt: pr.mergedAt,
			Checks: pr.checks(now, repo),
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
	headOid, err := git(repo.origin, "rev-parse", "refs/heads/"+branch)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	now := f.clock.Now()
	pr := &pullRequest{
		issue: is, number: f.next, draft: draft, state: gh.Open, baseRef: base, openedAt: now, headOid: headOid,
	}
	pr.startRun(now)
	f.prs[prKey(repo, branch)] = pr
	return nil
}

func (f *Forge) Ready(_ context.Context, repoPath, branch string) error {
	return f.update(repoPath, branch, func(pr *pullRequest) { pr.draft = false })
}

func (f *Forge) Edit(_ context.Context, repoPath, branch, base string) error {
	return f.update(repoPath, branch, func(pr *pullRequest) { pr.baseRef = base })
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

func (*Forge) CloseIssue(context.Context, string, string) error { return nil }

var _ gh.Forge = (*Forge)(nil)
