package view

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
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

type RepoRow struct {
	FullName      string
	Tracked       bool
	Path          string
	RefusedBranch string
	Project       string
	Steps         []CheckStep
}

type ReposPage struct {
	Chrome
	Groups          []RepoGroup
	NewProject      NewProjectForm
	LastImportError option.Option[ImportError]
	Query           string
	Search          *RepoSearch
}

type RepoGroup struct {
	Scope ScopeEntry
	Count int
	Rows  []RepoRow

	Editable bool

	Projects []string
	Hues     []int
}

type NewProjectForm struct {
	DefaultHue int
	Hues       []int
	Repos      []string
}

type RepoSearch struct {
	Rows        []RepoRow
	SearchError string
}

type RepoPage struct {
	Chrome
	Title           string
	Remote          string
	Known           bool
	Ready           bool
	Banner          Banner
	Features        []FeatureRow
	LastImportError option.Option[ImportError]
	Tickets         Board
}

func (p RepoPage) Path() string { return RepoPath(p.Title) }

func repoCrumbs(fullName string) []Crumb {
	return []Crumb{{Label: "Repos", Path: "/repos"}, {Label: fullName, Mono: true}}
}

func RepoPath(fullName string) string {
	return "/repos/" + fullName
}

func trackedRow(r store.Repo) RepoRow {
	return RepoRow{
		FullName: r.Name, Tracked: true, Path: RepoPath(r.Name), Project: r.Project,
		Steps: checkSteps(bannerState(r), r.RefusalKind),
	}
}

func (r *Reader) KnownRepo(ctx context.Context, scope string) (store.Repo, bool, error) {
	repos, err := r.store.Repos(ctx)
	if err != nil {
		return store.Repo{}, false, fmt.Errorf("load repos: %w", err)
	}
	for _, repo := range repos {
		if scope != "" && strings.EqualFold(repo.Name, scope) {
			return repo, true, nil
		}
	}
	return store.Repo{}, false, nil
}

func (r *Reader) Repos(ctx context.Context, now time.Time) (ReposPage, error) {
	f, err := r.loadFrame(ctx, now, Params{})
	if err != nil {
		return ReposPage{}, fmt.Errorf("load frame: %w", err)
	}
	chrome := f.chrome
	chrome.setCurrent(keyRepos)
	chrome.titled("Repos")
	chrome.Crumbs = []Crumb{{Label: "Repos"}}
	groups, form := repoGroups(f.repos, f.projectList)
	importErr, err := r.importError(ctx, now)
	if err != nil {
		return ReposPage{}, fmt.Errorf("import error: %w", err)
	}
	return ReposPage{Chrome: chrome, Groups: groups, NewProject: form, LastImportError: importErr}, nil
}

func (r *Reader) RepoGroups(ctx context.Context) ([]RepoGroup, error) {
	repos, err := r.store.Repos(ctx)
	if err != nil {
		return nil, fmt.Errorf("load repo groups: load repos: %w", err)
	}
	projects, err := r.store.Projects(ctx)
	if err != nil {
		return nil, fmt.Errorf("load repo groups: load projects: %w", err)
	}
	groups, _ := repoGroups(repos, projects)
	return groups, nil
}

func repoGroups(repos []store.Repo, projects []store.Project) ([]RepoGroup, NewProjectForm) {
	byName := make(map[string]store.Repo, len(repos))
	names := make([]string, 0, len(repos))
	for _, repo := range repos {
		byName[repo.Name] = repo
		names = append(names, repo.Name)
	}
	projectNames := make([]string, 0, len(projects))
	for _, p := range projects {
		projectNames = append(projectNames, p.Name)
	}
	var groups []RepoGroup
	for _, scope := range ScopeList(projects, repos) {
		members := []string{scope.Value}
		if scope.Kind == ScopeProject {
			i := slices.IndexFunc(projects, func(p store.Project) bool { return p.Name == scope.Value })
			members = projects[i].Repos
		}
		group := RepoGroup{
			Scope: scope, Count: len(members), Projects: projectNames, Hues: hues(),
			Editable: scope.Kind == ScopeProject,
		}
		for _, name := range members {
			group.Rows = append(group.Rows, trackedRow(byName[name]))
		}
		groups = append(groups, group)
	}
	form := NewProjectForm{DefaultHue: leastUsedHue(projects), Hues: hues(), Repos: names}
	return groups, form
}

func (r *Reader) SearchRepos(ctx context.Context, query string, pushable []gh.RepoSummary) ([]RepoRow, error) {
	tracked, err := r.store.Repos(ctx)
	if err != nil {
		return nil, fmt.Errorf("load repos: %w", err)
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

type RepoPageRequest struct {
	Scope    string
	Offered  []string
	Selected []string
}

func (r *Reader) RepoPage(ctx context.Context, now time.Time, req RepoPageRequest) (RepoPage, error) {
	scope, offered, selected := req.Scope, req.Offered, req.Selected
	chrome, err := r.Chrome(ctx, now, ParseParams(nil))
	if err != nil {
		return RepoPage{}, fmt.Errorf("load chrome: %w", err)
	}
	chrome.setCurrent(keyRepos)
	chrome.titled(scope)
	chrome.Crumbs = repoCrumbs(scope)
	page := RepoPage{Chrome: chrome, Title: scope}
	if page.Banner, err = r.Banner(ctx, scope); err != nil {
		return RepoPage{}, fmt.Errorf("load banner: %w", err)
	}

	repo, known, err := r.KnownRepo(ctx, scope)
	if err != nil {
		return page, fmt.Errorf("load known repo: %w", err)
	}
	if !known {
		return page, nil
	}
	tickets, err := r.store.Tickets(ctx)
	if err != nil {
		return RepoPage{}, fmt.Errorf("load tickets: %w", err)
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
	page.Crumbs = repoCrumbs(page.Title)
	page.Remote = repo.Remote
	page.Known = true
	page.Ready = repo.Phase == plan.RepoReady
	for _, f := range offered {
		page.Features = append(page.Features, FeatureRow{Feature: f, Imported: imported[f], OtherRepos: others[f]})
	}
	page.LastImportError, err = r.importError(ctx, now)
	if err != nil {
		return RepoPage{}, fmt.Errorf("import error: %w", err)
	}
	page.Tickets, err = r.Board(ctx, now, ParseParams(url.Values{"repo": {repo.Name}, "ticket": selected}))
	if err != nil {
		return RepoPage{}, fmt.Errorf("load board: %w", err)
	}
	return page, nil
}

func (r *Reader) importError(ctx context.Context, now time.Time) (option.Option[ImportError], error) {
	lastErr, failed, err := r.store.LastImportError(ctx)
	if err != nil {
		return option.None[ImportError](), fmt.Errorf("load last import error: %w", err)
	}
	if !failed {
		return option.None[ImportError](), nil
	}
	return option.Some(ImportError{
		Age: relative(now, lastErr.At).Age, Feature: lastErr.Feature, Message: lastErr.Message,
	}), nil
}
