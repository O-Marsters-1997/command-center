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
	Glyph         plan.GlyphWord
	Status        string
	OpenTickets   int
	Steps         []CheckStep
}

type ReposPage struct {
	Chrome
	Groups          []RepoGroup
	LastImportError option.Option[ImportError]
	Query           string
	Search          *RepoSearch
}

type RepoGroup struct {
	Scope ScopeEntry
	Count int
	Rows  []RepoRow
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
	Project         ProjectMark
	OpenTickets     int
	Base            string
	Projects        []ProjectMark
	NewProject      NewProjectForm
	Unimported      []FeatureRow
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

func trackedRow(r store.Repo, open int) RepoRow {
	state := bannerState(r)
	glyph, _ := bannerWords(state, r)
	steps := checkSteps(state, r.RefusalKind)
	return RepoRow{
		FullName: r.Name, Tracked: true, Path: RepoPath(r.Name), Project: r.Project,
		Glyph: glyph, Status: repoStatus(state, steps), OpenTickets: open, Steps: steps,
	}
}

func repoStatus(state BannerState, steps []CheckStep) string {
	switch state {
	case BannerReady:
		return "Checks passed"
	case BannerRefused:
		if i := slices.IndexFunc(steps, func(s CheckStep) bool { return s.Glyph == plan.GlyphFailed }); i >= 0 {
			return "Failed: " + steps[i].Label
		}
		return "Failed: checks"
	case BannerUnknown, BannerUntracked, BannerCloning:
	}
	return "Checking the repo on GitHub…"
}

func openByRepo(rows []Row) map[string]int {
	open := make(map[string]int)
	for _, row := range rows {
		if row.Glyph != plan.GlyphDone {
			open[row.Repo]++
		}
	}
	return open
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
	importErr, err := r.importError(ctx, now)
	if err != nil {
		return ReposPage{}, fmt.Errorf("import error: %w", err)
	}
	groups := repoGroups(f.repos, f.projectList, openByRepo(f.rows))
	return ReposPage{Chrome: chrome, Groups: groups, LastImportError: importErr}, nil
}

func (r *Reader) RepoGroups(ctx context.Context, now time.Time) ([]RepoGroup, error) {
	f, err := r.loadFrame(ctx, now, Params{})
	if err != nil {
		return nil, fmt.Errorf("load repo groups: %w", err)
	}
	return repoGroups(f.repos, f.projectList, openByRepo(f.rows)), nil
}

func repoGroups(repos []store.Repo, projects []store.Project, open map[string]int) []RepoGroup {
	byName := make(map[string]store.Repo, len(repos))
	var loose []string
	for _, repo := range repos {
		byName[repo.Name] = repo
		if repo.Project == "" {
			loose = append(loose, repo.Name)
		}
	}
	slices.Sort(loose)
	var groups []RepoGroup
	add := func(scope ScopeEntry, members []string) {
		if len(members) == 0 {
			return
		}
		group := RepoGroup{Scope: scope, Count: len(members)}
		for _, name := range members {
			group.Rows = append(group.Rows, trackedRow(byName[name], open[name]))
		}
		groups = append(groups, group)
	}
	for _, scope := range ScopeList(projects, nil) {
		i := slices.IndexFunc(projects, func(p store.Project) bool { return p.Name == scope.Value })
		add(scope, projects[i].Repos)
	}
	add(ScopeEntry{Label: "No project", Kind: ScopeRepo}, loose)
	return groups
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
			rows = append(rows, trackedRow(repo, 0))
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
	Scope   string
	Offered []string
}

func (r *Reader) RepoPage(ctx context.Context, now time.Time, req RepoPageRequest) (RepoPage, error) {
	scope, offered := req.Scope, req.Offered
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
	page.Project = ProjectMark{Name: repo.Project, Hue: repo.Hue}
	page.Base = "origin/" + plan.DefaultBaseBranch
	projects, err := r.store.Projects(ctx)
	if err != nil {
		return RepoPage{}, fmt.Errorf("load projects: %w", err)
	}
	for _, p := range projects {
		page.Projects = append(page.Projects, ProjectMark{Name: p.Name, Hue: p.Hue})
	}
	page.NewProject = NewProjectForm{DefaultHue: leastUsedHue(projects), Hues: hues(), Repos: []string{repo.Name}}
	for _, f := range offered {
		if !imported[f] {
			page.Unimported = append(page.Unimported, FeatureRow{Feature: f, OtherRepos: others[f]})
		}
	}
	page.LastImportError, err = r.importError(ctx, now)
	if err != nil {
		return RepoPage{}, fmt.Errorf("import error: %w", err)
	}
	page.Tickets, err = r.Board(ctx, now, ParseParams(url.Values{"repo": {repo.Name}}))
	if err != nil {
		return RepoPage{}, fmt.Errorf("load board: %w", err)
	}
	page.OpenTickets = openByRepo(rowsIn(page.Tickets.Groups))[repo.Name]
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
