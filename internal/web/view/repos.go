package view

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type FeatureRow struct {
	Feature    string
	Imported   bool
	OtherRepos []string
}

type ImportError struct {
	Age     string
	Feature string
	Message string
}

// RepoRow is one line of the repos page: a repo the app tracks, or a pushable one it does not.
type RepoRow struct {
	FullName      string
	Tracked       bool
	Path          string
	RefusedBranch string
	Steps         []CheckStep
}

// ReposPage is the /repos page: every repo the app knows.
type ReposPage struct {
	Chrome
	Results         RepoSearch
	LastImportError *ImportError
}

// RepoSearch is the fragment GET /repos/search swaps in. SearchError is set when gh failed, in
// which case Rows still hold the known repos that match.
type RepoSearch struct {
	Rows        []RepoRow
	SearchError string
}

// RepoPage is the /repos/{owner}/{name} page. Known is false for a repo the app does not track.
type RepoPage struct {
	Chrome
	Title           string
	Remote          string
	Known           bool
	Ready           bool
	Banner          Banner
	Features        []FeatureRow
	LastImportError *ImportError
	Tickets         Board
}

// RepoPath is the page of fullName, an owner/name pair.
func RepoPath(fullName string) string {
	return "/repos/" + fullName
}

func trackedRow(r store.Repo) RepoRow {
	return RepoRow{
		FullName: r.Name, Tracked: true, Path: RepoPath(r.Name),
		Steps: checkSteps(bannerState(r), r.RefusalKind),
	}
}

func (r *Reader) repoScope(ctx context.Context, scope string) (string, error) {
	if scope == "" {
		return "", nil
	}
	repos, err := r.store.Repos(ctx)
	if err != nil {
		return "", err
	}
	return normalizeRepoScope(scope, repos), nil
}

// KnownRepo finds the tracked repo whose owner/name is scope, ignoring case.
func (r *Reader) KnownRepo(ctx context.Context, scope string) (store.Repo, bool, error) {
	repos, err := r.store.Repos(ctx)
	if err != nil {
		return store.Repo{}, false, err
	}
	for _, repo := range repos {
		if scope != "" && strings.EqualFold(repo.Name, scope) {
			return repo, true, nil
		}
	}
	return store.Repo{}, false, nil
}

// Repos shapes the unscoped page: the tracked repos.
func (r *Reader) Repos(ctx context.Context, now time.Time) (ReposPage, error) {
	chrome, err := r.Chrome(ctx, now, ParseParams(nil))
	if err != nil {
		return ReposPage{}, err
	}
	chrome.Section = "repos"
	repos, err := r.store.Repos(ctx)
	if err != nil {
		return ReposPage{}, err
	}
	rows := make([]RepoRow, 0, len(repos))
	for _, repo := range repos {
		rows = append(rows, trackedRow(repo))
	}
	importErr, err := r.importError(ctx, now)
	if err != nil {
		return ReposPage{}, err
	}
	return ReposPage{Chrome: chrome, Results: RepoSearch{Rows: rows}, LastImportError: importErr}, nil
}

// SearchRepos merges the pushable repos whose name contains query with the tracked repos that
// match it. An empty query is the tracked repos alone.
func (r *Reader) SearchRepos(ctx context.Context, query string, pushable []gh.RepoSummary) ([]RepoRow, error) {
	tracked, err := r.store.Repos(ctx)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	rows := make([]RepoRow, 0, len(tracked))
	for _, repo := range tracked {
		if strings.Contains(strings.ToLower(repo.Name), q) {
			rows = append(rows, trackedRow(repo))
		}
	}
	if q == "" {
		return rows, nil
	}
	var untracked []RepoRow
	for _, p := range pushable {
		if !strings.Contains(strings.ToLower(p.FullName), q) || tracks(tracked, p) {
			continue
		}
		row := RepoRow{FullName: p.FullName, Path: RepoPath(p.FullName)}
		if p.DefaultBranch != plan.DefaultBaseBranch {
			row.RefusedBranch = p.DefaultBranch
		}
		untracked = append(untracked, row)
	}
	slices.SortFunc(untracked, func(a, b RepoRow) int { return strings.Compare(a.FullName, b.FullName) })
	return append(rows, untracked...), nil
}

func tracks(tracked []store.Repo, p gh.RepoSummary) bool {
	return slices.ContainsFunc(tracked, func(repo store.Repo) bool {
		return strings.EqualFold(repo.Name, p.FullName) || git.SameRemote(repo.Remote, p.SSHURL)
	})
}

// RepoPage shapes the scoped page. offered is the repo's own tracker's features; a feature
// imported with tickets in other repos names them.
func (r *Reader) RepoPage(
	ctx context.Context, now time.Time, scope string, offered, selected []string,
) (RepoPage, error) {
	chrome, err := r.Chrome(ctx, now, ParseParams(nil))
	if err != nil {
		return RepoPage{}, err
	}
	chrome.Section = "repos"
	chrome.RepoCrumb, chrome.RepoCrumbPath = scope, RepoPath(scope)
	page := RepoPage{Chrome: chrome, Title: scope}
	if page.Banner, err = r.Banner(ctx, scope); err != nil {
		return RepoPage{}, err
	}

	repo, known, err := r.KnownRepo(ctx, scope)
	if err != nil || !known {
		return page, err
	}
	tickets, err := r.store.Tickets(ctx)
	if err != nil {
		return RepoPage{}, err
	}
	imported := make(map[string]bool)
	others := make(map[string][]string)
	for _, t := range tickets {
		if t.Feature == "" {
			continue
		}
		if t.Repo == repo.Name {
			imported[t.Feature] = true
			continue
		}
		if !slices.Contains(others[t.Feature], t.Repo) {
			others[t.Feature] = append(others[t.Feature], t.Repo)
		}
	}
	for _, repos := range others {
		slices.Sort(repos)
	}

	page.Title = repo.Name
	page.RepoCrumb, page.RepoCrumbPath = page.Title, RepoPath(page.Title)
	page.Remote = repo.Remote
	page.Known = true
	page.Ready = repo.State == store.RepoReady
	for _, f := range offered {
		page.Features = append(page.Features, FeatureRow{Feature: f, Imported: imported[f], OtherRepos: others[f]})
	}
	page.LastImportError, err = r.importError(ctx, now)
	if err != nil {
		return RepoPage{}, err
	}
	page.Tickets, err = r.Board(ctx, now, ParseParams(url.Values{"repo": {repo.Name}, "ticket": selected}))
	if err != nil {
		return RepoPage{}, err
	}
	return page, nil
}

func (r *Reader) importError(ctx context.Context, now time.Time) (*ImportError, error) {
	lastErr, failed, err := r.store.LastImportError(ctx)
	if err != nil || !failed {
		return nil, err
	}
	return &ImportError{Age: relative(now, lastErr.At).Age, Feature: lastErr.Feature, Message: lastErr.Message}, nil
}
