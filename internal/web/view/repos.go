package view

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/git"
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
	FullName string
	Tracked  bool
	Path     string
}

// ReposPage is the unscoped /features page: every repo the app knows.
type ReposPage struct {
	Chrome
	Results         RepoSearch
	LastImportError *ImportError
}

// RepoSearch is the fragment GET /features/search swaps in. SearchError is set when gh failed, in
// which case Rows still hold the known repos that match.
type RepoSearch struct {
	Rows        []RepoRow
	SearchError string
}

// RepoPage is the scoped /features?repo= page. Known is false for a repo the app does not track.
type RepoPage struct {
	Chrome
	Title           string
	Remote          string
	BoardPath       string
	Known           bool
	Features        []FeatureRow
	LastImportError *ImportError
}

func repoPath(fullName string) string {
	q := url.Values{"repo": {fullName}}.Encode()
	return "/features?" + strings.ReplaceAll(q, "%2F", "/")
}

// fullName is a repo's owner/name, read from its remote; a path repo has none and keeps its name.
func fullName(r config.Repo) string {
	remote := git.NormaliseRemote(r.Remote)
	if _, rest, ok := strings.Cut(remote, "/"); ok && r.Remote != "" {
		return rest
	}
	return r.Name
}

func trackedRow(r config.Repo) RepoRow {
	name := fullName(r)
	return RepoRow{FullName: name, Tracked: true, Path: repoPath(name)}
}

// KnownRepo finds the configured repo that scope names, by owner/name or by configured name.
func (r *Reader) KnownRepo(scope string) (config.Repo, bool) {
	for _, repo := range r.repos {
		if scope != "" && (strings.EqualFold(fullName(repo), scope) || repo.Name == scope) {
			return repo, true
		}
	}
	return config.Repo{}, false
}

// Repos shapes the unscoped page: the configured repos, each tracked.
func (r *Reader) Repos(ctx context.Context, now time.Time) (ReposPage, error) {
	chrome, err := r.Chrome(ctx, now, ParseParams(nil))
	if err != nil {
		return ReposPage{}, err
	}
	chrome.Section = "repos"
	rows := make([]RepoRow, 0, len(r.repos))
	for _, repo := range r.repos {
		rows = append(rows, trackedRow(repo))
	}
	importErr, err := r.importError(ctx, now)
	if err != nil {
		return ReposPage{}, err
	}
	return ReposPage{Chrome: chrome, Results: RepoSearch{Rows: rows}, LastImportError: importErr}, nil
}

// SearchRepos merges the pushable repos whose name contains query with the configured repos that
// match it. An empty query is the configured repos alone.
func (r *Reader) SearchRepos(query string, pushable []gh.RepoSummary) []RepoRow {
	q := strings.ToLower(strings.TrimSpace(query))
	rows := make([]RepoRow, 0, len(r.repos))
	for _, repo := range r.repos {
		if row := trackedRow(repo); strings.Contains(strings.ToLower(row.FullName), q) {
			rows = append(rows, row)
		}
	}
	if q == "" {
		return rows
	}
	var untracked []RepoRow
	for _, p := range pushable {
		if !strings.Contains(strings.ToLower(p.FullName), q) || r.tracks(p) {
			continue
		}
		untracked = append(untracked, RepoRow{FullName: p.FullName, Path: repoPath(p.FullName)})
	}
	slices.SortFunc(untracked, func(a, b RepoRow) int { return strings.Compare(a.FullName, b.FullName) })
	return append(rows, untracked...)
}

func (r *Reader) tracks(p gh.RepoSummary) bool {
	return slices.ContainsFunc(r.repos, func(repo config.Repo) bool {
		return repo.Remote != "" && git.SameRemote(repo.Remote, p.SSHURL)
	})
}

// RepoPage shapes the scoped page. offered is the repo's own tracker's features; a feature
// imported with tickets in other repos names them.
func (r *Reader) RepoPage(ctx context.Context, now time.Time, scope string, offered []string) (RepoPage, error) {
	chrome, err := r.Chrome(ctx, now, ParseParams(nil))
	if err != nil {
		return RepoPage{}, err
	}
	chrome.Section = "repos"
	chrome.RepoCrumb, chrome.RepoCrumbPath = scope, repoPath(scope)
	page := RepoPage{Chrome: chrome, Title: scope}

	repo, known := r.KnownRepo(scope)
	if !known {
		return page, nil
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
		imported[t.Feature] = true
		if t.Repo != repo.Name && !slices.Contains(others[t.Feature], t.Repo) {
			others[t.Feature] = append(others[t.Feature], t.Repo)
		}
	}
	for _, repos := range others {
		slices.Sort(repos)
	}

	page.Title = fullName(repo)
	page.Chrome.RepoCrumb, page.Chrome.RepoCrumbPath = page.Title, repoPath(page.Title)
	page.Remote = repo.Remote
	page.BoardPath = Params{Repo: repo.Name}.pagePath()
	page.Known = true
	for _, f := range offered {
		page.Features = append(page.Features, FeatureRow{Feature: f, Imported: imported[f], OtherRepos: others[f]})
	}
	page.LastImportError, err = r.importError(ctx, now)
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
