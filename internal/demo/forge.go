package demo

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/gh"
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
	return pr.ciOutcome() == ciHang || now.Before(pr.runStart.Add(time.Duration(pr.issue.CIAfter)))
}

func (pr *pullRequest) greenAt(now time.Time) bool {
	return !pr.running(now) && pr.ciOutcome() == ciPass && pr.issue.Compat != ciFail
}

func (pr *pullRequest) checks(now time.Time, repo *sandboxRepo) map[string]gh.CheckState {
	checks := map[string]gh.CheckState{ciCheck: pr.check(now, repo.scenarioName, ciCheck, pr.ciOutcome())}
	if repo.compatCheck != "" {
		outcome := ciPass
		if pr.issue.Compat == ciFail {
			outcome = ciFail
		}
		checks[repo.compatCheck] = pr.check(now, repo.scenarioName, repo.compatCheck, outcome)
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

// Advance closes every open pull request whose scripted close time has come, and merges every
// open, non-draft one whose scripted merge time has come and whose checks are green.
func (f *Forge) Advance() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.clock.Now()
	prs := slices.SortedFunc(maps.Values(f.prs), func(a, b *pullRequest) int { return cmp.Compare(a.number, b.number) })
	for _, pr := range prs {
		if pr.state != gh.Open {
			continue
		}
		if closeAfter := pr.issue.Close.After; closeAfter > 0 {
			if !now.Before(pr.openedAt.Add(time.Duration(closeAfter))) {
				pr.state = gh.Closed
			}
			continue
		}
		due := pr.openedAt.Add(time.Duration(pr.issue.Merge.After))
		if pr.draft || now.Before(due) || !pr.greenAt(now) {
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

// Rerun starts the next entry of the ticket's ci sequence on the pull request whose check run
// has id runID.
func (f *Forge) Rerun(_ context.Context, repoPath, runID string) error {
	number, err := strconv.Atoi(runID)
	if err != nil {
		return fmt.Errorf("run id %q: %w", runID, err)
	}
	repo, err := f.sb.repoFor(repoPath)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, pr := range f.prs {
		if pr.issue.repo == repo && pr.number == number {
			pr.startRun(f.clock.Now())
			return nil
		}
	}
	return fmt.Errorf("no pull request with run %s", runID)
}

func (*Forge) RunViewLogFailed(context.Context, string, string) (string, error) {
	return "", nil
}

func (*Forge) CloseIssue(context.Context, string, string) error { return nil }

var _ gh.Forge = (*Forge)(nil)
