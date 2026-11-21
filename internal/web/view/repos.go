package view

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type ImportOffer struct {
	Feature  string
	Imported bool
	Tickets  int
}

type ImportState string

const (
	ImportAvailable ImportState = "available"
	ImportRunning   ImportState = "running"
	ImportDone      ImportState = "done"
)

type FeatureOffer struct {
	Feature    string
	State      ImportState
	Tickets    int
	Ready      int
	Repo       string
	Path       string
	OtherRepos []string
}

func (o FeatureOffer) LaunchPath() string { return featureLaunchPath(o.Feature) }

func (o FeatureOffer) ID() string {
	return "offer-" + strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return '-'
	}, o.Repo+"-"+o.Feature)
}

type SwatchSet struct {
	Hues    []int
	Checked int
}

func (n NewProjectForm) Swatches() SwatchSet { return SwatchSet{Hues: n.Hues, Checked: n.DefaultHue} }

func (g RepoGroup) Swatches() SwatchSet { return SwatchSet{Hues: hues(), Checked: g.Scope.Hue} }

func (g RepoGroup) Anchor() string { return "p-" + g.Scope.Value }

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
	Badge         string
	Steps         []CheckStep
	Offers        []FeatureOffer
}

type ReposPage struct {
	Chrome
	ProjectsPane
	LastImportError option.Option[ImportError]
	Query           string
	Search          option.Option[RepoSearch]
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

type ProjectsPane struct {
	Groups     []RepoGroup
	Projects   []ProjectMark
	NewProject NewProjectForm
}

type RepoSearch struct {
	Query       string
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
	LastImportError option.Option[ImportError]
	Tickets         Board
}

func (p RepoPage) Path() string { return RepoPath(p.Title) }

func repoCrumbs(fullName string) []Crumb {
	return []Crumb{{Label: "Projects", Path: "/repos"}, {Label: fullName, Mono: true}}
}

func RepoPath(fullName string) string {
	return "/repos/" + fullName
}

func trackedRow(r store.Repo, open int) RepoRow {
	state := bannerState(r)
	glyph, _ := bannerWords(state, r)
	steps := checkSteps(state, r)
	return RepoRow{
		FullName: r.Name, Tracked: true, Path: RepoPath(r.Name), Project: r.Project,
		Glyph: glyph, Status: repoStatus(state, steps), OpenTickets: open, Badge: repoBadge(state),
	}
}

func repoBadge(state BannerState) string {
	switch state {
	case BannerReady:
		return "Ready"
	case BannerRefused:
		return "Refused"
	case BannerUnknown, BannerUntracked, BannerCloning:
	}
	return "Checking"
}

func (r *Reader) SearchHit(ctx context.Context, repo string) (RepoRow, error) {
	b, err := r.Banner(ctx, repo)
	if err != nil {
		return RepoRow{}, fmt.Errorf("load banner: %w", err)
	}
	return RepoRow{FullName: b.Repo, Tracked: true, Path: RepoPath(b.Repo), Glyph: b.Glyph, Badge: repoBadge(b.State)}, nil
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
		if plan.OpenGlyph(row.Glyph) {
			open[row.Repo]++
		}
	}
	return open
}

func knownRepo(repos []store.Repo, scope string) (store.Repo, bool) {
	for _, repo := range repos {
		if scope != "" && strings.EqualFold(repo.Name, scope) {
			return repo, true
		}
	}
	return store.Repo{}, false
}

func (r *Reader) Repos(ctx context.Context, now time.Time, offers map[string][]ImportOffer) (ReposPage, error) {
	f, err := r.loadFrame(ctx, now, Params{})
	if err != nil {
		return ReposPage{}, fmt.Errorf("load frame: %w", err)
	}
	chrome := f.chrome
	chrome.setCurrent(keyRepos)
	chrome.titled("Projects")
	chrome.Crumbs = []Crumb{{Label: "Projects"}}
	importErr, err := r.importError(ctx, now)
	if err != nil {
		return ReposPage{}, fmt.Errorf("import error: %w", err)
	}
	pane, err := r.projectsPane(ctx, f, offers)
	if err != nil {
		return ReposPage{}, err
	}
	return ReposPage{Chrome: chrome, ProjectsPane: pane, LastImportError: importErr}, nil
}

func (r *Reader) projectsPane(ctx context.Context, f frame, offers map[string][]ImportOffer) (ProjectsPane, error) {
	intents, err := r.store.PendingVerbIntents(ctx, plan.VerbImport)
	if err != nil {
		return ProjectsPane{}, fmt.Errorf("load pending imports: %w", err)
	}
	pane := ProjectsPane{
		Groups:     repoGroups(f, intents, offers),
		NewProject: NewProjectForm{DefaultHue: leastUsedHue(f.projectList), Hues: hues()},
	}
	for _, p := range f.projectList {
		pane.Projects = append(pane.Projects, ProjectMark{Name: p.Name, Hue: p.Hue})
	}
	for _, repo := range f.repos {
		pane.NewProject.Repos = append(pane.NewProject.Repos, repo.Name)
	}
	slices.Sort(pane.NewProject.Repos)
	return pane, nil
}

func repoGroups(f frame, intents []store.VerbIntent, offers map[string][]ImportOffer) []RepoGroup {
	open := openByRepo(f.rows)
	byName := make(map[string]store.Repo, len(f.repos))
	var loose []string
	for _, repo := range f.repos {
		byName[repo.Name] = repo
		if repo.Project == "" {
			loose = append(loose, repo.Name)
		}
	}
	slices.Sort(loose)
	var groups []RepoGroup
	add := func(scope ScopeEntry, members []string) {
		group := RepoGroup{Scope: scope, Count: len(members)}
		for _, name := range members {
			row := trackedRow(byName[name], open[name])
			if byName[name].Phase == plan.RepoReady {
				row.Offers = featureOffers(f, byName[name], offers, intents)
			}
			group.Rows = append(group.Rows, row)
		}
		groups = append(groups, group)
	}
	for _, scope := range ScopeList(f.projectList, nil) {
		i := slices.IndexFunc(f.projectList, func(p store.Project) bool { return p.Name == scope.Value })
		add(scope, f.projectList[i].Repos)
	}
	if len(loose) > 0 {
		add(ScopeEntry{Label: "No project", Kind: ScopeRepo}, loose)
	}
	return groups
}

const maxSearchHits = 6

func featureOffers(
	f frame, repo store.Repo, all map[string][]ImportOffer, intents []store.VerbIntent,
) []FeatureOffer {
	offers := make([]FeatureOffer, 0, len(all[repo.Name]))
	for _, in := range all[repo.Name] {
		offer := FeatureOffer{
			Feature: in.Feature, Tickets: in.Tickets, Repo: repo.Name, Path: featureLaunchPath(in.Feature),
			State: ImportAvailable,
		}
		if in.Imported {
			offer.State = ImportDone
		}
		if slices.ContainsFunc(intents, func(i store.VerbIntent) bool {
			return i.TicketID == in.Feature && (i.Payload == "" || i.Payload == repo.Name)
		}) {
			offer.State = ImportRunning
		}
		for _, row := range f.rows {
			if row.Feature == in.Feature && row.Repo == repo.Name && row.Glyph == plan.GlyphReady {
				offer.Ready++
			}
		}
		for other, theirs := range all {
			if other != repo.Name && slices.ContainsFunc(theirs, func(o ImportOffer) bool { return o.Feature == in.Feature }) {
				offer.OtherRepos = append(offer.OtherRepos, other)
			}
		}
		slices.Sort(offer.OtherRepos)
		offers = append(offers, offer)
	}
	return offers
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
	rows = append(rows, untracked...)
	return rows[:min(len(rows), maxSearchHits)], nil
}

func tracks(tracked []store.Repo, p gh.RepoSummary) bool {
	return slices.ContainsFunc(tracked, func(repo store.Repo) bool {
		return strings.EqualFold(repo.Name, p.FullName) || git.SameRemote(repo.Remote, p.SSHURL)
	})
}

func (r *Reader) RepoPage(ctx context.Context, now time.Time, scope string) (RepoPage, error) {
	board, err := r.Board(ctx, now, ParseParams(url.Values{"repo": {scope}}))
	if err != nil {
		return RepoPage{}, fmt.Errorf("load board: %w", err)
	}
	chrome := board.Chrome
	chrome.setCurrent(keyRepos)
	chrome.titled(scope)
	chrome.Crumbs = repoCrumbs(scope)
	page := RepoPage{Chrome: chrome, Title: scope}
	pending, err := r.store.TrackPending(ctx, scope)
	if err != nil {
		return RepoPage{}, fmt.Errorf("track pending: %w", err)
	}
	repo, known := knownRepo(board.repos, scope)
	page.Banner = bannerOf(scope, pending, repo, known)
	if !known {
		return page, nil
	}
	page.Title = repo.Name
	page.Crumbs = repoCrumbs(page.Title)
	page.Remote = repo.Remote
	page.Known = true
	page.Ready = repo.Phase == plan.RepoReady
	page.Project = ProjectMark{Name: repo.Project, Hue: repo.Hue}
	page.Base = "origin/" + plan.DefaultBaseBranch
	page.LastImportError, err = r.importError(ctx, now)
	if err != nil {
		return RepoPage{}, fmt.Errorf("import error: %w", err)
	}
	page.Tickets = board
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
