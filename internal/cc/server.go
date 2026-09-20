package cc

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"maps"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

//go:embed page.tmpl
var pageSource string

//go:embed assets all:assets/dist
var assetsDir embed.FS

var page = template.Must(template.New("page").
	Funcs(template.FuncMap{
		"head": func(r *row, scope, featureScope string) rowSlot {
			return newRowSlot(*r, true, 0, scope, featureScope)
		},
		"child": func(r row, depth int, scope, featureScope string) rowSlot {
			return newRowSlot(r, false, depth, scope, featureScope)
		},
		"destructive": func(verb string) bool { _, ok := destructiveVerbs[verb]; return ok },
		"percent":     func(part, total int) int { return percentOf(part, total) },
	}).
	Parse(pageSource))

//go:embed band.tmpl
var bandSource string

// The blank identifier is deliberate, not dead code: this registers "band" into page's own tree,
// and page.tmpl and boardswap.tmpl both call it by name via {{template "band" .}}.
var _ = template.Must(page.New("band").Parse(bandSource))

//go:embed board.tmpl
var boardSource string

var boardFragment = template.Must(page.New("board").Parse(boardSource))

//go:embed masthead.tmpl
var mastheadSource string

// The blank identifier is deliberate, not dead code: this registers "masthead" into the shared
// tree that page.tmpl and boardSwap both call it from.
var _ = template.Must(page.New("masthead").Parse(mastheadSource))

//go:embed layout.tmpl
var layoutSource string

// The blank identifier is deliberate, not dead code: this registers "docHead", "topbar",
// "sidebar" and the icon-* templates into the shared tree every page renders through --
// page.tmpl, features.tmpl, preview.tmpl and confirm.tmpl all call them by name, so the doctype,
// head and the chrome outside every hx-swap target are written once.
var _ = template.Must(page.New("layout").Parse(layoutSource))

//go:embed boardswap.tmpl
var boardSwapSource string

// boardSwap answers every poll and every verb: the table htmx swaps into the target, plus the
// masthead and band as out-of-band swaps.
var boardSwap = template.Must(page.New("boardSwap").Parse(boardSwapSource))

//go:embed detail.tmpl
var detailSource string

// The blank identifier is deliberate, not dead code: this registers "detail" into boardFragment's
// own tree, and board.tmpl calls it by name via {{template "detail" .}}.
var _ = template.Must(boardFragment.New("detail").Parse(detailSource))

// rowSlot carries LaunchVerb, CancelVerb, Scope and FeatureScope because html/template resets $ to
// the invoked subtemplate's own argument, so "row" cannot see pageView's copies -- Scope and
// FeatureScope are board.tmpl's own RepoScope and FeatureScope, to name a row that differs from them.
type rowSlot struct {
	row
	Head         bool
	Depth        int
	LaunchVerb   string
	CancelVerb   string
	FollowUpVerb string
	Scope        string
	FeatureScope string
}

func newRowSlot(r row, head bool, depth int, scope, featureScope string) rowSlot {
	return rowSlot{
		row: r, Head: head, Depth: depth,
		LaunchVerb: plan.VerbLaunch, CancelVerb: plan.VerbCancel, FollowUpVerb: plan.VerbFollowUp,
		Scope: scope, FeatureScope: featureScope,
	}
}

//go:embed features.tmpl
var featuresSource string

// pathEscape joins page's shared FuncMap: Funcs adds to the tree's one function map regardless of
// which member template it is called on, so head, child, destructive and percent see it too.
var featuresPage = template.Must(page.New("features").
	Funcs(template.FuncMap{"pathEscape": url.PathEscape}).
	Parse(featuresSource))

//go:embed launch_modal.tmpl
var launchModalSource string

var launchModal = template.Must(template.New("launchModal").Parse(launchModalSource))

// Server is the status page plus the launch, launch-authorisation and features routes. It
// never writes the database directly except to queue an intent: every state it shows is derived
// from tickets and the last observation at render time (§5, inv. 14).
type Server struct {
	store             *Store
	now               func() time.Time
	repos             []Repo
	stackingByRepo    map[string]bool
	checksByRepo      map[string]verdict.Predicate
	mergifySHAByRepo  map[string]string
	compatCheckByRepo map[string]string
	dataDir           string
	spend             *spendCache
	trackerFor        TrackerSource
	mux               *http.ServeMux
	nudge             func()
}

// NewServer assembles the page and its routes over a store, a clock, the configured repos and
// the data directory: stacking, the verdict predicate, the mergify hash and the compat check
// name are all per-repo config, and dataDir is the fleet the header names.
func NewServer(store *Store, now func() time.Time, repos []Repo, dataDir string) *Server {
	s := &Server{
		store: store, now: now, repos: repos, dataDir: dataDir, spend: newSpendCache(), trackerFor: tracker.New,
		stackingByRepo: stackingByRepo(repos), checksByRepo: checksByRepo(repos),
		mergifySHAByRepo: mergifySHAByRepo(repos), compatCheckByRepo: compatCheckByRepo(repos),
		nudge: func() {},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /board", s.handleBoard)
	mux.HandleFunc("GET /graph.json", s.handleGraph)
	mux.HandleFunc("GET /insights", s.handleInsightsPage)
	mux.HandleFunc("GET /insights.json", s.handleInsights)
	mux.Handle("GET /assets/", http.FileServerFS(assetsDir))
	mux.HandleFunc("GET /assets/app.css", s.handleStylesheet)
	mux.HandleFunc("GET /ticket/{ticket}/log", s.handleLog)
	mux.HandleFunc("GET /features", s.handleFeatures)
	mux.HandleFunc("GET /features/{feature}", s.handleFeatureRedirect)
	mux.HandleFunc("POST /features/{feature}/import", requireBrowserOrigin(s.handleImportFeature))
	mux.HandleFunc("GET /launch/candidates", s.handleCandidates)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /confirm", s.handleConfirm)
	mux.HandleFunc("POST /launch/open", requireBrowserOrigin(s.handleLaunchOpen))
	mux.HandleFunc("POST /launch", requireBrowserOrigin(s.handleLaunch))
	mux.HandleFunc("POST /verb", requireBrowserOrigin(s.handleVerb))
	mux.HandleFunc("POST /ticket", requireBrowserOrigin(s.handleTicket))
	s.mux = mux
	return s
}

// SetTrackerSource replaces the server's tracker.New, so a test can drive GET /features with a
// fake source rather than shelling out to gh.
func (s *Server) SetTrackerSource(resolve TrackerSource) { s.trackerFor = resolve }

// SetNudge replaces the server's nudge call. App.New wires this to Loop.Nudge once, after both
// exist, which is how the server queues an import intent and wakes the loop without importing
// the loop package itself.
func (s *Server) SetNudge(nudge func()) { s.nudge = nudge }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) handleStylesheet(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, assetsDir, "assets/dist/app.css")
}

// requireBrowserOrigin rejects any request whose Origin header does not name this server's own
// host. Comparing against r.Host rather than a fixed allowlist is what makes this work under
// the e2e harness's ephemeral ports. Any future mutating verb wraps its handler the same way.
func requireBrowserOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			http.Error(w, "the Origin header is required", http.StatusForbidden)
			return
		}
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			http.Error(w, "the Origin header does not match this server", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// row's json tags are what GET /graph.json serves: the route marshals []group verbatim, so this
// is the graph island's only view of the data too (docs/prds/prd-fleet-view.md § One derivation).
type row struct {
	URL string `json:"url"`
	// Repo names the row's own configured repo, so a group kept whole by a member in scope can
	// still name where an out-of-scope sibling lives (CONTEXT.md § Scope, ADR 11).
	Repo string `json:"repo"`
	// Feature names the row's own tracker feature, so a group kept whole by a member in scope can
	// still name where an out-of-scope sibling lives (CONTEXT.md § Feature).
	Feature string `json:"feature"`
	// Title is empty when the tick's read did not cover the ticket: a fresh DB, or an issue past
	// `gh issue list`'s own 100-row limit.
	Title      string `json:"title"`
	State      string `json:"state"`
	Reason     string `json:"reason"`
	Tone       string `json:"tone"`
	Unattended bool   `json:"unattended"`
	Alive      bool   `json:"alive"`
	// Verbs comes from internal/plan: which verbs a state offers is a decision, so it is table-
	// tested beside plan.Status rather than spelled out per state in the template.
	Verbs        []string `json:"verbs"`
	Branch       string   `json:"branch"`
	PendingVerbs []string `json:"pending_verbs"`
	Base         string   `json:"base"`
	// BaseVerdict is the base's own CI verdict label ("review_me"/"needs_you"/"checking"/
	// "base_moved"), empty for a root row: a red check on a descendant whose base moved may not
	// be its own fault (docs/designs/command-centre-design.md § 4a).
	BaseVerdict string `json:"base_verdict"`
	// StackDepth and MergeOrder are the row's distance from a root and the order it merges in,
	// bottom-up — the app never merges a PR, so this is the only place the order is shown
	// (docs/prds/prd-command-centre.md § The page → stack order).
	StackDepth int `json:"stack_depth"`
	MergeOrder int `json:"merge_order"`
	// Warning is invariant 2's hazard, named on the row: a non-main-based PR carrying
	// ready-to-merge would squash-merge into its parent branch with the parent's own checks
	// unseen, and empty otherwise. The app never applies that label itself.
	Warning  string   `json:"warning"`
	Blocking []string `json:"blocking"`
	Worktree string   `json:"worktree"`
	PRNumber int      `json:"pr_number"`
	PRState  string   `json:"pr_state"`
	// Pgid, Elapsed and LogPath are plain, copy-pasteable text (docs/prds/prd-command-centre.md §
	// The page) — empty for a ticket with no run yet.
	Pgid           string  `json:"pgid"`
	Elapsed        string  `json:"elapsed"`
	ElapsedSeconds int     `json:"elapsed_seconds"`
	ElapsedPercent int     `json:"elapsed_percent"`
	LogPath        string  `json:"log_path"`
	CancelCount    int     `json:"cancel_count"`
	SpendTokens    int     `json:"spend_tokens"`
	SpendUSD       float64 `json:"spend_usd"`
	SpendSettled   bool    `json:"spend_settled"`
	// BaselineSHA and Checks are the detail fragment's, not the board's: the row is derived once
	// and every island reads it (docs/prds/prd-operator-surface.md § One derivation).
	BaselineSHA string  `json:"baseline_sha"`
	Checks      []check `json:"checks"`
	RedChecks   []check `json:"red_checks"`
	// Draft mirrors the PR's own observed isDraft, not DraftGate's own opinion: a failed gh pr
	// ready leaves GitHub's real state unchanged, and this must still render honestly.
	Draft       bool   `json:"draft"`
	DraftReason string `json:"draft_reason"`

	// Selected is this render's ?sel= row: the only one whose detail <tr> exists at all, so an
	// unattached hx-preserve id never lingers past the row that grew it
	// (docs/prds/prd-fleet-view.md § The hx-preserve id is conditional on selection).
	Selected bool `json:"selected"`
	Checked  bool `json:"checked"`
	// SelectPath/SelectPush toggle Selected; TogglePath/TogglePush toggle Checked. Each pair is
	// the board fragment to hx-get and the root path to hx-push-url.
	SelectPath string `json:"select_path"`
	SelectPush string `json:"select_push"`
	TogglePath string `json:"toggle_path"`
	TogglePush string `json:"toggle_push"`
	// VerbPath is /verb carrying this render's view state, so the swap handleVerb answers with
	// rebuilds the board the operator was looking at rather than the default one.
	VerbPath string `json:"verb_path"`
	// Log is the parsed run log, set only when Selected (docs/prds/prd-fleet-view.md § The run log).
	Log logDetail `json:"log"`
}

type check struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"details_url"`
}

func (r row) Ticket() string { return ticketRef(r.URL) }

func ticketRef(url string) string { return "#" + path.Base(url) }

// FollowUpAvailable reports whether this row's state offers follow-up.
func (r row) FollowUpAvailable() bool { return slices.Contains(r.Verbs, plan.VerbFollowUp) }

func (r row) Stack() string {
	if r.Base == "" || r.Base == defaultBaseBranch {
		return fmt.Sprintf("L%d", r.MergeOrder)
	}
	return fmt.Sprintf("L%d ← %s", r.MergeOrder, r.Base)
}

func (r row) ChecksPassed() int {
	passed := 0
	for _, c := range r.Checks {
		if c.Conclusion == "SUCCESS" {
			passed++
		}
	}
	return passed
}

// DetailID is the row's stable DOM id. A ticket URL is neither a usable id nor a CSS selector,
// and htmx needs both: hx-target resolves the selector, and hx-preserve matches the id to keep
// an expanded detail mounted through the board's own five-second swap.
func (r row) DetailID() string {
	sum := sha256.Sum256([]byte(r.URL))
	return "detail-" + hex.EncodeToString(sum[:6])
}

// ageView is a relative time the server renders and the page's clock keeps current. Stamp is the
// instant the browser counts from, and is empty when there is none to count from.
type ageView struct {
	Age   string
	Stamp string
}

type tickErrorView struct {
	Age     ageView
	Message string
}

// group is one blocker and the rows waiting on it. A row with no blocker in the ticket set is its
// own group with a nil Root (docs/prds/prd-operator-surface.md § Reading the board).
type group struct {
	Root     *row  `json:"root"`
	Children []row `json:"children"`
}

// chrome is the shell every page wears: workspace, the live and observe pills, the last tick's
// error, the current board/graph view, and the repo/feature scope links. layout.tmpl's topbar and
// masthead render it once per page load; every page's view model embeds it.
type chrome struct {
	Workspace  string
	LiveAgents int
	Observe    ageView
	// ObserveStale is decided here rather than in the template, which cannot compare durations.
	ObserveStale bool
	LastError    *tickErrorView
	// View picks which of board and graph page.tmpl shows; parseViewParams defaults it to board.
	View string
	// Section names the sidebar's current destination: "board", "graph" or "features". It tracks
	// View except on /features, which has no ?view= of its own.
	Section string
	// RepoScope is this render's normalised ?repo= value, empty when unscoped. The board's own
	// row template reads it to name a kept group's out-of-scope member (CONTEXT.md § Scope).
	RepoScope string
	// RepoLinks is the breadcrumb's own repo switcher (CONTEXT.md § Scope), empty when no repo is
	// configured so the breadcrumb renders no switcher at all.
	RepoLinks []scopeLink
	// FeatureScope is this render's normalised ?feature= value, empty when unscoped. The board's
	// own row template reads it to name a kept group's out-of-scope member (CONTEXT.md § Feature).
	FeatureScope string
	// FeatureImportPath is the breadcrumb's reimport action, set only when FeatureScope names one
	// feature to reimport.
	FeatureImportPath string
	// FeatureQuery is FeatureScope, url.QueryEscape'd for the sidebar's board/graph links to carry
	// the scope across views; empty whenever FeatureScope is.
	FeatureQuery string
}

type pageView struct {
	chrome
	Groups []group
	Band   bandView
	// BoardPath feeds back into the board's own hx-get, so the next poll and the next swap both
	// perpetuate this render's view state without the shell being involved.
	BoardPath string
}

// scopeLink is one breadcrumb switcher entry: "all" plus one per configured repo.
type scopeLink struct {
	Name    string
	Path    string
	Current bool
}

const observeStaleAfter = 20 * time.Second

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.renderView(w, r, page)
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	s.renderView(w, r, boardSwap)
}

// handleGraph serves pageView.Groups verbatim: the same []group the board template ranges over,
// json-tagged rather than reshaped, so the graph island lays out exactly what the board renders
// (docs/prds/prd-fleet-view.md § One derivation).
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	view, err := s.render(r.Context(), parseViewParams(r.URL.Query()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(view.Groups); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) renderView(w http.ResponseWriter, r *http.Request, tmpl *template.Template) {
	view, err := s.render(r.Context(), parseViewParams(r.URL.Query()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) render(ctx context.Context, params viewParams) (pageView, error) {
	params.Repo = normalizeRepoScope(params.Repo, s.stackingByRepo)

	tickets, err := s.store.Tickets(ctx)
	if err != nil {
		return pageView{}, err
	}
	params.Feature = normalizeFeatureScope(params.Feature, distinctFeatures(tickets))
	obs, observed, err := s.store.LastObservation(ctx)
	if err != nil {
		return pageView{}, err
	}
	lastErr, failed, err := s.store.LastError(ctx)
	if err != nil {
		return pageView{}, err
	}
	facts, vd, err := s.loadTicketFacts(ctx)
	if err != nil {
		return pageView{}, err
	}

	now := s.now()
	rows := derive(tickets, obs, facts, vd, s.stackingByRepo, now)
	applySpend(rows, s.spend)
	applyViewState(rows, params)
	groups := filterGroupsByFeature(filterGroupsByRepo(groupRows(rows), params.Repo), params.Feature)
	view := pageView{
		chrome:    s.buildChrome(tickets, obs, observed, lastErr, failed, now, params),
		Groups:    groups,
		Band:      deriveBand(rowsIn(groups)),
		BoardPath: params.boardPath(),
	}
	return view, nil
}

// buildChrome derives the shell every page wears from facts its caller already holds: render
// already fetched tickets, the observation and the last error for its own board derivation, and
// chromeFor fetches them fresh for the three pages that otherwise never touch the store for them.
func (s *Server) buildChrome(
	tickets []Ticket, obs Observation, observed bool, lastErr TickError, failed bool, now time.Time,
	params viewParams,
) chrome {
	c := chrome{
		Workspace:    workspaceName(s.dataDir),
		LiveAgents:   liveAgents(tickets, obs),
		Observe:      ageView{Age: "never"},
		ObserveStale: true,
		View:         params.View,
		Section:      params.View,
		RepoScope:    params.Repo,
		RepoLinks:    repoLinksFor(s.repos, params),
		FeatureScope: params.Feature,
	}
	if observed {
		c.Observe = relative(now, obs.ObservedAt)
		c.ObserveStale = now.Sub(obs.ObservedAt) >= observeStaleAfter
	}
	if failed && (!observed || lastErr.At.After(obs.ObservedAt)) {
		c.LastError = &tickErrorView{Age: relative(now, lastErr.At), Message: lastErr.Message}
	}
	if params.Feature != "" {
		c.FeatureImportPath = params.featureImportPath()
		c.FeatureQuery = url.QueryEscape(params.Feature)
	}
	return c
}

// chromeFor builds a page's chrome without render's board derivation, groups or verdicts -- the
// one cheap read /features, /preview and /confirm now make for the workspace, live and observe
// pills, and scope links that every page's topbar and masthead show.
func (s *Server) chromeFor(ctx context.Context, params viewParams) (chrome, error) {
	tickets, err := s.store.Tickets(ctx)
	if err != nil {
		return chrome{}, err
	}
	params.Repo = normalizeRepoScope(params.Repo, s.stackingByRepo)
	params.Feature = normalizeFeatureScope(params.Feature, distinctFeatures(tickets))

	obs, observed, err := s.store.LastObservation(ctx)
	if err != nil {
		return chrome{}, err
	}
	lastErr, failed, err := s.store.LastError(ctx)
	if err != nil {
		return chrome{}, err
	}
	return s.buildChrome(tickets, obs, observed, lastErr, failed, s.now(), params), nil
}

// applyViewState parses the selected row's own log only, not every row's: a board of twenty-five
// rows must not open twenty-five log files to render one poll.
func applyViewState(rows []row, params viewParams) {
	for i := range rows {
		r := &rows[i]
		r.Selected = params.Sel == r.URL
		r.Checked = slices.Contains(params.Tickets, r.URL)
		toggledSel := params.toggleSel(r.URL)
		r.SelectPath, r.SelectPush = toggledSel.boardPath(), toggledSel.pagePath()
		toggledTicket := params.toggleTicket(r.URL)
		r.TogglePath, r.TogglePush = toggledTicket.boardPath(), toggledTicket.pagePath()
		r.VerbPath = params.verbPath()
		if r.Selected {
			r.Log = buildLogDetail(r.LogPath, r.Alive, r.URL, params)
		}
	}
}

// loadTicketFacts gathers the ticketFacts and verdictDeps both render and handlePreview need.
func (s *Server) loadTicketFacts(ctx context.Context) (ticketFacts, verdictDeps, error) {
	memberships, err := s.store.LaunchMemberships(ctx)
	if err != nil {
		return ticketFacts{}, verdictDeps{}, err
	}
	latestRuns, err := s.store.LatestRunsByTicket(ctx)
	if err != nil {
		return ticketFacts{}, verdictDeps{}, err
	}
	pushFacts, err := s.store.PushFacts(ctx)
	if err != nil {
		return ticketFacts{}, verdictDeps{}, err
	}
	refreshFacts, err := s.store.RefreshFacts(ctx)
	if err != nil {
		return ticketFacts{}, verdictDeps{}, err
	}
	pendingVerbs, err := s.store.PendingIntentsByTicket(ctx)
	if err != nil {
		return ticketFacts{}, verdictDeps{}, err
	}
	removals, err := s.store.RemovalRefusals(ctx)
	if err != nil {
		return ticketFacts{}, verdictDeps{}, err
	}
	vd, err := verdictDepsFor(ctx, s.store, s.checksByRepo, s.mergifySHAByRepo, s.compatCheckByRepo)
	if err != nil {
		return ticketFacts{}, verdictDeps{}, err
	}

	facts := ticketFacts{
		memberships: memberships, latestRuns: latestRuns,
		pushes: pushFacts, refreshes: refreshFacts, pendingVerbs: pendingVerbs, removals: removals,
	}
	return facts, vd, nil
}

type verdictDeps struct {
	pushRows          map[string]PushRow
	checkingTicks     map[string]int
	checksByRepo      map[string]verdict.Predicate
	mergifySHAByRepo  map[string]string
	compatCheckByRepo map[string]string
}

// verdictDepsFor gathers the store-derived facts applyVerdict needs, over the caller's own
// per-repo predicate and mergify-hash maps -- shared by the page's render and the loop's own
// verdict-transition and draft-gate tick steps, which all compute the same verdict the same way
// (docs/designs/command-centre-design.md § 11 inv. 11).
func verdictDepsFor(
	ctx context.Context, store *Store, checksByRepo map[string]verdict.Predicate, mergifySHAByRepo map[string]string,
	compatCheckByRepo map[string]string,
) (verdictDeps, error) {
	pushRows, err := store.LatestPushes(ctx)
	if err != nil {
		return verdictDeps{}, err
	}
	checkingTicks, err := store.CheckingTicks(ctx)
	if err != nil {
		return verdictDeps{}, err
	}
	return verdictDeps{
		pushRows: pushRows, checkingTicks: checkingTicks,
		checksByRepo: checksByRepo, mergifySHAByRepo: mergifySHAByRepo,
		compatCheckByRepo: compatCheckByRepo,
	}, nil
}

// ticketFacts is the durable per-ticket state a row is derived from, keyed by ticket URL.
type ticketFacts struct {
	memberships  map[string]LaunchMembership
	latestRuns   map[string]RunSummary
	pushes       map[string]PushFact
	refreshes    map[string]RefreshFact
	pendingVerbs map[string][]string
	removals     map[string]string
}

// derive labels every row from the stored facts plus this tick's observation. No status is
// stored: facts are stored, labels are derived every tick
// (docs/designs/command-centre-design.md § Schema, inv. 14).
func derive(
	tickets []Ticket, obs Observation, facts ticketFacts, vd verdictDeps,
	stackingByRepo map[string]bool, now time.Time,
) []row {
	byURL := planTicketsByURL(tickets)
	prs := prsByBranch(tickets, obs)
	conflictingPeer := conflictingPeerHold(tickets, byURL, prs, stackingByRepo, obs)

	rows := make([]row, 0, len(tickets))
	verdictLabelByBranch := make(map[string]string, len(tickets))
	baseByBranch := make(map[string]string, len(tickets))
	for _, t := range tickets {
		pt := planTicket(t)
		unlock := plan.Unlocked(pt, byURL, prs, stackingByRepo[t.Repo])
		runFact, pgid, elapsed, elapsedSeconds, logPath := runFactFor(t, obs, facts, vd, conflictingPeer, now)
		membership := facts.memberships[t.URL]
		latestRun := facts.latestRuns[t.URL]
		state, reason := plan.Status(plan.Facts{
			Ticket:          pt,
			Unlock:          unlock,
			Now:             now,
			Authorised:      membership.LaunchID != 0,
			LatestRun:       runFact,
			CancelledMember: membership.Cancelled,
			ConflictedBase:  conflictedBase(pt, byURL, unlock, stackingByRepo[t.Repo], obs),
		})
		verdictLabelByBranch[t.Branch] = verdictLabel(runFact)
		baseByBranch[t.Branch] = unlock.BaseBranch
		pr := obs.PRs[branchKey(t.Repo, t.Branch)]
		var redLeaves []string
		if runFact != nil {
			redLeaves = runFact.RedLeaves
		}
		rows = append(rows, row{
			URL:            t.URL,
			Repo:           t.Repo,
			Feature:        t.Feature,
			Title:          obs.Titles[t.URL],
			State:          state.String(),
			Reason:         string(reason),
			Tone:           plan.Tone(state),
			Unattended:     state.Unattended(),
			Alive:          obs.Runs[t.URL].Alive,
			Verbs:          plan.Verbs(state),
			PendingVerbs:   facts.pendingVerbs[t.URL],
			Branch:         t.Branch,
			Base:           unlock.BaseBranch,
			Worktree:       obs.Worktrees[branchKey(t.Repo, t.Branch)],
			PRNumber:       pr.Number,
			PRState:        pr.State.String(),
			Pgid:           pgid,
			Elapsed:        elapsed,
			ElapsedSeconds: elapsedSeconds,
			LogPath:        logPath,
			CancelCount:    membership.Members,
			Warning:        cmp.Or(readyToMergeWarning(pr), removalWarning(state, facts.removals[t.URL])),
			BaselineSHA:    latestRun.BaselineSHA,
			Checks:         sortedChecks(pr.Checks),
			RedChecks:      redChecksFor(redLeaves, pr.Checks),
			Blocking:       unlock.Blocking,
			Draft:          pr.IsDraft,
			DraftReason:    draftReasonFor(pr, pt, byURL, prs, runFact),
		})
	}

	longestElapsed := 0
	for _, r := range rows {
		longestElapsed = max(longestElapsed, r.ElapsedSeconds)
	}
	// A second pass: a row's base is another row's own branch (never main, §4a), so its verdict
	// and its depth are only known once every row above has been built.
	for i := range rows {
		if rows[i].Base != "" && rows[i].Base != defaultBaseBranch {
			rows[i].BaseVerdict = verdictLabelByBranch[rows[i].Base]
		}
		rows[i].StackDepth = plan.StackDepth(rows[i].Branch, baseByBranch)
		rows[i].MergeOrder = rows[i].StackDepth + 1
		rows[i].ElapsedPercent = percentOf(rows[i].ElapsedSeconds, longestElapsed)
	}
	return rows
}

func percentOf(part, total int) int {
	if total <= 0 {
		return 0
	}
	return part * 100 / total
}

func sortedChecks(checks map[string]gh.CheckState) []check {
	out := make([]check, 0, len(checks))
	for _, name := range slices.Sorted(maps.Keys(checks)) {
		cs := checks[name]
		out = append(out, check{Name: name, Status: cs.Status, Conclusion: cs.Conclusion, DetailsURL: cs.DetailsURL})
	}
	return out
}

func redChecksFor(redLeaves []string, checks map[string]gh.CheckState) []check {
	if len(redLeaves) == 0 {
		return nil
	}
	red := make(map[string]bool, len(redLeaves))
	for _, name := range redLeaves {
		red[name] = true
	}
	out := make([]check, 0, len(redLeaves))
	for _, c := range sortedChecks(checks) {
		if red[c.Name] {
			out = append(out, c)
		}
	}
	return out
}

// groupRows keys a fan-in row's group on the first blocker in its own Blocking
// (internal/plan/plan.go:119); Reason still names every blocker.
func groupRows(rows []row) []group {
	byURL := make(map[string]row, len(rows))
	childrenByRoot := make(map[string][]row, len(rows))
	for _, r := range rows {
		byURL[r.URL] = r
		if len(r.Blocking) > 0 {
			root := r.Blocking[0]
			childrenByRoot[root] = append(childrenByRoot[root], r)
		}
	}

	isChild := make(map[string]bool, len(rows))
	for _, children := range childrenByRoot {
		for _, c := range children {
			isChild[c.URL] = true
		}
	}

	rootURLs := make([]string, 0, len(childrenByRoot))
	for root := range childrenByRoot {
		if !isChild[root] {
			rootURLs = append(rootURLs, root)
		}
	}
	slices.Sort(rootURLs)

	groups := make([]group, 0, len(rootURLs)+len(rows))
	for _, root := range rootURLs {
		children := flattenChain(root, childrenByRoot)
		slices.SortFunc(children, func(a, b row) int {
			if a.MergeOrder != b.MergeOrder {
				return cmp.Compare(a.MergeOrder, b.MergeOrder)
			}
			return cmp.Compare(a.URL, b.URL)
		})
		rootRow := byURL[root]
		groups = append(groups, group{Root: &rootRow, Children: children})
	}

	var ungrouped []row
	for _, r := range rows {
		if len(r.Blocking) == 0 {
			if _, isRoot := childrenByRoot[r.URL]; !isRoot {
				ungrouped = append(ungrouped, r)
			}
		}
	}
	slices.SortFunc(ungrouped, func(a, b row) int { return cmp.Compare(a.URL, b.URL) })
	for _, r := range ungrouped {
		groups = append(groups, group{Children: []row{r}})
	}
	return groups
}

func flattenChain(root string, childrenByRoot map[string][]row) []row {
	var out []row
	seen := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, c := range childrenByRoot[u] {
			if seen[c.URL] {
				continue
			}
			seen[c.URL] = true
			out = append(out, c)
			queue = append(queue, c.URL)
		}
	}
	return out
}

// filterGroupsByRepo narrows groups to a repo scope, admitting a matching group whole: keeping a
// group's root or any child out of it would leave groupRows' unchecked byURL[root] lookup
// pointing at nothing (ADR 11 "a scope admits a group whole"). An empty repo is unscoped.
func filterGroupsByRepo(groups []group, repo string) []group {
	if repo == "" {
		return groups
	}
	filtered := make([]group, 0, len(groups))
	for _, g := range groups {
		if groupInRepo(g, repo) {
			filtered = append(filtered, g)
		}
	}
	return filtered
}

func groupInRepo(g group, repo string) bool {
	if g.Root != nil && g.Root.Repo == repo {
		return true
	}
	for _, c := range g.Children {
		if c.Repo == repo {
			return true
		}
	}
	return false
}

// filterGroupsByFeature narrows groups to a feature scope, following filterGroupsByRepo: it
// admits a matching group whole, and an empty feature is unscoped. Composed with
// filterGroupsByRepo in render, so both axes narrow independently rather than one overriding the
// other.
func filterGroupsByFeature(groups []group, feature string) []group {
	if feature == "" {
		return groups
	}
	filtered := make([]group, 0, len(groups))
	for _, g := range groups {
		if groupInFeature(g, feature) {
			filtered = append(filtered, g)
		}
	}
	return filtered
}

func groupInFeature(g group, feature string) bool {
	if g.Root != nil && g.Root.Feature == feature {
		return true
	}
	for _, c := range g.Children {
		if c.Feature == feature {
			return true
		}
	}
	return false
}

// rowsIn flattens groups back to the rows they render, which is what deriveBand counts: a scoped
// group still shows its out-of-scope members, so the band counts them too (ADR 11).
func rowsIn(groups []group) []row {
	rows := make([]row, 0, len(groups))
	for _, g := range groups {
		if g.Root != nil {
			rows = append(rows, *g.Root)
		}
		rows = append(rows, g.Children...)
	}
	return rows
}

// repoLinksFor is the breadcrumb's repo switcher: "all" plus one entry per configured repo, nil
// when none are configured so a single-repo fixture's breadcrumb renders no switcher at all.
func repoLinksFor(repos []Repo, params viewParams) []scopeLink {
	if len(repos) == 0 {
		return nil
	}
	links := make([]scopeLink, 0, len(repos)+1)
	links = append(links, scopeLink{Name: "all", Path: params.withRepo("").pagePath(), Current: params.Repo == ""})
	for _, r := range repos {
		links = append(links,
			scopeLink{Name: r.Name, Path: params.withRepo(r.Name).pagePath(), Current: params.Repo == r.Name})
	}
	return links
}

// distinctFeatures is the sorted set of non-blank Feature values among tickets, which render also
// uses to blank an unrecognised ?feature=.
func distinctFeatures(tickets []Ticket) []string {
	seen := make(map[string]bool)
	for _, t := range tickets {
		if t.Feature != "" {
			seen[t.Feature] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// baseVerdict is the preview's read of a stacked row's base before authorising: empty for main,
// otherwise the base ticket's own CI verdict, exactly what the main page shows once the row has
// launched (docs/designs/command-centre-design.md § 4b, "you are about to build on a red parent").
func baseVerdict(
	base string, ticketsByBranch map[string]Ticket, obs Observation, facts ticketFacts, vd verdictDeps, now time.Time,
) string {
	if base == "" || base == defaultBaseBranch {
		return ""
	}
	baseTicket, ok := ticketsByBranch[base]
	if !ok {
		return ""
	}
	// nil: this preview label never reflects a peer hold, only the base's own run and verdict.
	runFact, _, _, _, _ := runFactFor(baseTicket, obs, facts, vd, nil, now)
	return verdictLabel(runFact)
}

// readyToMergeWarning names invariant 2's hazard for the page: pr.BaseRef is what GitHub
// actually has the PR targeting, which is what matters here, not the base this app would itself
// choose (docs/designs/command-centre-design.md § 4a inv. 2).
func readyToMergeWarning(pr gh.PR) string {
	if !plan.StackedReadyToMergeWarning(pr.BaseRef, pr.Labels) {
		return ""
	}
	return fmt.Sprintf(
		"ready-to-merge on a non-main base (%s): would squash into the parent branch, checks unseen", pr.BaseRef)
}

// removalWarning surfaces the row's own remove-worktree refusal, only while its state still
// offers the verb -- once the row leaves that state (removed, or no longer eligible), a refusal
// from before must never render as if it were today's (docs/adr/0012-cc-proves-what-tp-cannot.md).
func removalWarning(s plan.State, detail string) string {
	if detail == "" || !slices.Contains(plan.Verbs(s), plan.VerbRemoveWorktree) {
		return ""
	}
	return "worktree removal refused: " + detail
}

// draftReasonFor names why a drafted row is still a draft: plan.DraftGate's own reason, or --
// when the gate says ready but pr.IsDraft is still true -- that the last `gh pr ready` call
// failed and the next tick retries (docs/designs/command-centre-design.md § 6 job 2, inv. 13).
func draftReasonFor(
	pr gh.PR, t plan.Ticket, byURL map[string]plan.Ticket, prs map[string]plan.PRState, runFact *plan.RunFact,
) string {
	if !pr.IsDraft {
		return ""
	}
	gating := plan.GatingBlockers(t, byURL)
	verdictGreen := runFact != nil && runFact.VerdictReviewMe
	draft, reason := plan.DraftGate(gating, prs, verdictGreen)
	if !draft {
		return "ready to un-draft; the last gh pr ready call has not taken effect yet"
	}
	return string(reason)
}

// runFactFor builds plan.Status's LatestRun input for one ticket, plus the pgid, elapsed time and
// log path the page renders. Push facts only count once the run's outcome is push, and PROpen
// reads this tick's PR snapshot rather than a stored column (inv. 14).
func runFactFor(
	t Ticket, obs Observation, facts ticketFacts, vd verdictDeps, conflictingPeer map[string]string, now time.Time,
) (runFact *plan.RunFact, pgid, elapsed string, elapsedSeconds int, logPath string) {
	summary, ok := facts.latestRuns[t.URL]
	if !ok {
		return nil, "", "", 0, ""
	}

	fact := &plan.RunFact{LogPath: summary.LogPath, Alive: obs.Runs[t.URL].Alive}
	// plan.go's inv. 14 treats PRMerged as a fact fetched every tick regardless of Outcome, so it
	// is set here unconditionally too, not only on the Outcome==Push path below.
	ownState := obs.PRs[branchKey(t.Repo, t.Branch)].State
	fact.PROpen = ownState == gh.Open
	fact.PRMerged = ownState == gh.Merged
	fact.PRClosedUnmerged = ownState == gh.Closed
	if summary.HasOutcome {
		fact.HasOutcome = true
		fact.Outcome = summary.Outcome
		if summary.Outcome == plan.OutcomePush {
			pf := facts.pushes[t.URL]
			fact.PushRefused = pf.Refused
			fact.PushRefusedPath = pf.RefusedPath
			fact.PushFailed = pf.Failed
			rf := facts.refreshes[t.URL]
			fact.RefreshRefused = rf.Refused
			fact.RefreshRefusedReason = plan.Reason(rf.Reason)
			fact.VerificationFailed = rf.VerificationFailed
			fact.VerificationFailedReason = plan.Reason(rf.VerificationFailedDetail)
			fact.MidMerge = obs.MidMerge[branchKey(t.Repo, t.Branch)]
			if obs.ConflictsWithBase[branchKey(t.Repo, t.Branch)] {
				fact.ConflictsWithMain = true
				fact.ConflictsWithMainReason = plan.Reason(
					fmt.Sprintf("%s no longer merges cleanly into main", t.Branch))
			}
			fact.ConflictingPeer = conflictingPeer[t.URL]
			if fact.PROpen && !fact.PushRefused && !fact.PushFailed {
				applyVerdict(fact, t, obs, vd)
			}
		}
		if summary.Outcome == plan.OutcomeFailed && summary.Kind == runKindResolve &&
			obs.MidMerge[branchKey(t.Repo, t.Branch)] {
			fact.Resolved = true
		}
	}

	logPath = summary.LogPath
	if summary.Pgid != nil {
		pgid = strconv.Itoa(*summary.Pgid)
	}
	if fact.Alive && summary.ProcStartedAt != nil {
		d := now.Sub(*summary.ProcStartedAt).Round(time.Second)
		elapsed = d.String()
		elapsedSeconds = int(d.Seconds())
	}
	return fact, pgid, elapsed, elapsedSeconds, logPath
}

// defaultBaseBranch mirrors internal/plan's own unexported copy: verdict's import guard (like
// plan's) forbids depending on that package for one string constant.
const defaultBaseBranch = "main"

// branchKey names one branch in every branch-keyed map on Observation. Two configured repos can
// hold the same branch name, and "//" can never appear in a real git branch name, so this key
// never collides across repos the way the plain name would.
func branchKey(repo, branch string) string { return repo + "//" + branch }

// mainTipKey names defaultBaseBranch's own tip in Observation.BranchTips, main being every
// unstacked ticket's base (issue #85: main's own tip is checked exactly like a still-stacked
// base's, not exempted, since retargetOne can re-point a row onto it).
func mainTipKey(repo string) string { return branchKey(repo, defaultBaseBranch) }

// applyVerdict fills in a pushed, open-PR run's CI verdict, if the repo has opted into one:
// unconfigured [repo.checks] leaves fact untouched, which is what keeps every pre-Phase-5
// fixture reading exactly as it did before this phase (statusFromPush's own PROpen fallback).
func applyVerdict(fact *plan.RunFact, t Ticket, obs Observation, vd verdictDeps) {
	predicate := vd.checksByRepo[t.Repo]
	if predicate.IsZero() {
		return
	}
	pushRow, pushed := vd.pushRows[t.URL]
	if !pushed {
		return // disposed push-outcome this same tick, before push.go recorded the row
	}

	pr := obs.PRs[branchKey(t.Repo, t.Branch)]
	hasRecordedBase := pushRow.BaseBranch != ""
	mergifySHA := vd.mergifySHAByRepo[t.Repo]

	result := verdict.Evaluate(predicate, verdict.Input{
		Checks:       verdictChecks(pr.Checks),
		HeadOidMatch: pushRow.PushedTip != "" && pr.HeadOid == pushRow.PushedTip,
		StackedBase:  hasRecordedBase,
		BaseSHAMatch: obs.BranchTips[branchKey(t.Repo, pushRow.BaseBranch)] == pushRow.BaseSHAAtPush,
		ConfigHashOK: mergifySHA == "" || obs.MergifyHash[t.Repo] == mergifySHA,
		PushedAt:     pushRow.PushedAt,
		Now:          pushRow.PushedAt.Add(time.Duration(vd.checkingTicks[t.URL]) * tickPeriod),
		AuthorLogin:  pr.AuthorLogin,
		CompatCheck:  vd.compatCheckByRepo[t.Repo],
	})

	switch result.Verdict {
	case verdict.ReviewMe:
		fact.VerdictReviewMe = true
	case verdict.WaitingOnProducerDeploy:
		fact.VerdictWaitingOnProducer = true
	case verdict.NeedsYou:
		if checkActuallyFailed := len(result.RedLeaves) > 0; checkActuallyFailed {
			fact.VerdictCIFailed = true
			fact.RedLeaves = result.RedLeaves
		} else {
			fact.VerdictNeedsYou = true
		}
	case verdict.BaseMoved:
		fact.VerdictBaseMoved = true
	case verdict.Checking:
		// leave every flag false; VerdictReason below still carries the sentence.
	}
	fact.VerdictReason = plan.Reason(result.Reason)
}

// verdictChecks maps gh's normalised check shape onto verdict's own -- the pure package cannot
// import internal/gh (issue #2 AC12), so this is the one place the two vocabularies meet.
func verdictChecks(checks map[string]gh.CheckState) map[string]verdict.CheckState {
	out := make(map[string]verdict.CheckState, len(checks))
	for name, c := range checks {
		out[name] = toVerdictCheckState(c)
	}
	return out
}

// toVerdictCheckState mirrors the "no retry-pending rule" call (docs/designs/command-centre-design.md § 8):
// anything completed but not exactly SUCCESS or SKIPPED reads as a definite Failure, never a
// third kind of maybe.
func toVerdictCheckState(cs gh.CheckState) verdict.CheckState {
	if cs.Status != "COMPLETED" {
		return verdict.Pending
	}
	switch cs.Conclusion {
	case "SUCCESS":
		return verdict.Success
	case "SKIPPED":
		return verdict.Skipped
	default:
		return verdict.Failure
	}
}

// handleEvents dumps the append-only audit log as JSON: what reconstructs the whole run, every
// authorisation, launch, disposition, push and refusal (docs/prds/prd-command-centre.md § Phase 4).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.Events(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(events); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type candidate struct {
	URL         string   `json:"url"`
	Ref         string   `json:"ref"`
	Title       string   `json:"title"`
	Repo        string   `json:"repo"`
	Feature     string   `json:"feature"`
	Label       string   `json:"label"`
	Reason      string   `json:"reason"`
	Base        string   `json:"base"`
	BaseVerdict string   `json:"base_verdict"`
	PromptHash  string   `json:"prompt_hash"`
	BlockedBy   []string `json:"blocked_by"`
	Prompt      string   `json:"-"`
}

type candidateInputs struct {
	tickets []Ticket
	obs     Observation
	facts   ticketFacts
	vd      verdictDeps
}

func (s *Server) loadCandidateInputs(ctx context.Context) (candidateInputs, error) {
	tickets, err := s.store.Tickets(ctx)
	if err != nil {
		return candidateInputs{}, err
	}
	obs, _, err := s.store.LastObservation(ctx)
	if err != nil {
		return candidateInputs{}, err
	}
	facts, vd, err := s.loadTicketFacts(ctx)
	if err != nil {
		return candidateInputs{}, err
	}
	return candidateInputs{tickets: tickets, obs: obs, facts: facts, vd: vd}, nil
}

func candidatesFor(
	requested []string, in candidateInputs, stackingByRepo map[string]bool, now time.Time,
) ([]candidate, error) {
	byTicketURL := make(map[string]Ticket, len(in.tickets))
	ticketsByBranch := make(map[string]Ticket, len(in.tickets))
	for _, t := range in.tickets {
		byTicketURL[t.URL] = t
		ticketsByBranch[t.Branch] = t
	}
	byURL := planTicketsByURL(in.tickets)

	slice := make(map[string]bool, len(requested))
	for _, ticketURL := range requested {
		if _, ok := byURL[ticketURL]; !ok {
			return nil, fmt.Errorf("unknown ticket %q", ticketURL)
		}
		slice[ticketURL] = true
	}

	prs := prsByBranch(in.tickets, in.obs)

	candidates := make([]candidate, 0, len(requested))
	for _, ticketURL := range requested {
		t := byTicketURL[ticketURL]
		pt := byURL[ticketURL]
		stacking := stackingByRepo[t.Repo]
		unlock := plan.Unlocked(pt, byURL, prs, stacking)
		label, reason := plan.Preview(
			unlock, slice, in.facts.memberships[ticketURL].LaunchID,
			conflictedBase(pt, byURL, unlock, stacking, in.obs))

		base := unlock.BaseBranch
		if base == "" {
			base = plan.ProspectiveBase(pt, byURL, stacking)
		}
		composed := plan.Compose(pt)
		candidates = append(candidates, candidate{
			URL: t.URL, Ref: ticketRef(t.URL), Title: t.Title, Repo: t.Repo, Feature: t.Feature,
			Label: label.String(), Reason: string(reason),
			Base:        "origin/" + base,
			BaseVerdict: baseVerdict(base, ticketsByBranch, in.obs, in.facts, in.vd, now),
			PromptHash:  plan.Hash(composed), BlockedBy: t.BlockedBy, Prompt: composed,
		})
	}
	return candidates, nil
}

func candidateSelection(q url.Values, tickets []Ticket) ([]string, error) {
	if feature := q.Get("feature"); feature != "" {
		var selected []string
		for _, t := range tickets {
			if t.Feature == feature {
				selected = append(selected, t.URL)
			}
		}
		return selected, nil
	}
	requested := q["ticket"]
	if len(requested) == 0 {
		return nil, fmt.Errorf("either ?feature= or at least one ?ticket= is required")
	}
	return requested, nil
}

type launchModalView struct {
	Feature      string
	FeatureQuery string
	TicketQuery  string
	Pending      bool
	Refused      string
	Empty        bool
}

func (s *Server) buildFeatureLaunchModalView(ctx context.Context, feature string) (launchModalView, error) {
	view := launchModalView{Feature: feature, FeatureQuery: url.QueryEscape(feature)}

	intents, err := s.store.PendingVerbIntents(ctx, importVerb)
	if err != nil {
		return launchModalView{}, err
	}
	for _, intent := range intents {
		if intent.TicketID == feature {
			view.Pending = true
			return view, nil
		}
	}

	in, err := s.loadCandidateInputs(ctx)
	if err != nil {
		return launchModalView{}, err
	}
	requested, err := candidateSelection(url.Values{"feature": {feature}}, in.tickets)
	if err != nil {
		return launchModalView{}, err
	}
	candidates, err := candidatesFor(requested, in, s.stackingByRepo, s.now())
	if err != nil {
		return launchModalView{}, err
	}

	if len(candidates) == 0 {
		lastErr, failed, err := s.store.LastImportError(ctx)
		if err != nil {
			return launchModalView{}, err
		}
		if failed && lastErr.Feature == feature {
			view.Refused = lastErr.Message
		} else {
			view.Empty = true
		}
		return view, nil
	}

	return view, nil
}

func (s *Server) buildTicketLaunchModalView(ctx context.Context, requested []string) (launchModalView, error) {
	in, err := s.loadCandidateInputs(ctx)
	if err != nil {
		return launchModalView{}, err
	}
	if _, err := candidatesFor(requested, in, s.stackingByRepo, s.now()); err != nil {
		return launchModalView{}, err
	}
	return launchModalView{TicketQuery: candidateQuery(requested)}, nil
}

func candidateQuery(requested []string) string {
	q := make(url.Values, len(requested))
	for _, ticketURL := range requested {
		q.Add("ticket", ticketURL)
	}
	return q.Encode()
}

func (s *Server) renderLaunchModal(w http.ResponseWriter, view launchModalView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := launchModal.Execute(w, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleLaunchOpen(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	if feature := r.FormValue("feature"); feature != "" {
		if err := QueueImport(ctx, s.store, feature, s.now()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.nudge()

		view, err := s.buildFeatureLaunchModalView(ctx, feature)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.renderLaunchModal(w, view)
		return
	}

	requested := r.Form["ticket"]
	if len(requested) == 0 {
		http.Error(w, "either feature or at least one ticket is required", http.StatusBadRequest)
		return
	}
	view, err := s.buildTicketLaunchModalView(ctx, requested)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.renderLaunchModal(w, view)
}

func (s *Server) handleCandidates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Header.Get("HX-Request") != "" {
		q := r.URL.Query()
		if feature := q.Get("feature"); feature != "" {
			view, err := s.buildFeatureLaunchModalView(ctx, feature)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			s.renderLaunchModal(w, view)
			return
		}
		requested := q["ticket"]
		if len(requested) == 0 {
			http.Error(w, "either ?feature= or at least one ?ticket= is required", http.StatusBadRequest)
			return
		}
		view, err := s.buildTicketLaunchModalView(ctx, requested)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.renderLaunchModal(w, view)
		return
	}

	in, err := s.loadCandidateInputs(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	requested, err := candidateSelection(r.URL.Query(), in.tickets)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	candidates, err := candidatesFor(requested, in, s.stackingByRepo, s.now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(candidates); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleLaunch queues one launch intent per requested ticket, all sharing one fresh group token
// so the next tick's ApplyLaunchIntents recognises them as a single authorisation. It does not
// re-check Preview's Refused case: an authorised ticket whose blocker sits outside the slice
// simply stays queued forever with an honest reason (§ A launch).
func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// ParseForm merges the posted body with the query string, so one checkbox per launchable row
	// and a hand-built `POST /launch?ticket=...&ticket=...` are the same repeated field to r.Form.
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	requested := r.Form["ticket"]
	if len(requested) == 0 {
		http.Error(w, "at least one ticket is required", http.StatusBadRequest)
		return
	}
	// One `hash` field per launchable row, each naming its own ticket: an unchecked row still posts
	// its hidden hash, so pairing by position would pair the survivors wrong.
	previewed := make(map[string]string, len(r.Form["hash"]))
	for _, field := range r.Form["hash"] {
		ticketURL, hash, ok := strings.Cut(field, " ")
		if !ok {
			http.Error(w, fmt.Sprintf("malformed hash field %q, want \"<ticket> <hash>\"", field),
				http.StatusBadRequest)
			return
		}
		previewed[ticketURL] = hash
	}

	tickets, err := s.store.Tickets(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	byURL := planTicketsByURL(tickets)
	hashes := make(map[string]string, len(requested))
	for _, ticketURL := range requested {
		t, ok := byURL[ticketURL]
		if !ok {
			http.Error(w, fmt.Sprintf("unknown ticket %q", ticketURL), http.StatusBadRequest)
			return
		}
		hash := plan.Hash(plan.Compose(t))
		if want, ok := previewed[ticketURL]; ok && want != hash {
			http.Error(w, fmt.Sprintf("ticket %s was previewed at hash %s and now composes to %s",
				ticketURL, want, hash), http.StatusConflict)
			return
		}
		hashes[ticketURL] = hash
	}

	group, err := randomGroup()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := s.now()
	for _, ticketURL := range requested {
		if err := s.store.QueueLaunchIntent(ctx, ticketURL, hashes[ticketURL], group, now); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleVerb queues one verb intent against one ticket — a handler only ever does this single
// blind INSERT; the loop is the sole reader and actor on it (inv. 9, see loop.go's
// applyKillIntents, push.go's applyRetryPushIntents and verbs.go's re-run/close-pr/
// remove-worktree appliers).
func (s *Server) handleVerb(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// FormValue, not URL.Query: the page's per-row form posts both fields in the body, and
	// reading the form falls back to the query string, which keeps a hand-built
	// `POST /verb?verb=kill&ticket=...` working unchanged.
	verb := r.FormValue("verb")
	ticketURL := r.FormValue("ticket")
	if verb == "" || ticketURL == "" {
		http.Error(w, "verb and ticket are both required", http.StatusBadRequest)
		return
	}
	if !supportedVerbs[verb] {
		http.Error(w, fmt.Sprintf("unsupported verb %q", verb), http.StatusBadRequest)
		return
	}

	var prompt string
	if verb == followUpVerb {
		prompt = strings.TrimSpace(r.FormValue("prompt"))
		if prompt == "" {
			http.Error(w, "prompt is required for follow-up", http.StatusBadRequest)
			return
		}
	}

	tickets, err := s.store.Tickets(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, ok := planTicketsByURL(tickets)[ticketURL]; !ok {
		http.Error(w, fmt.Sprintf("unknown ticket %q", ticketURL), http.StatusBadRequest)
		return
	}

	if verb == followUpVerb {
		err = s.store.QueueVerbIntentWithPayload(ctx, ticketURL, verb, prompt, s.now())
	} else {
		err = s.store.QueueVerbIntent(ctx, ticketURL, verb, s.now())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The swap shows the row's "<verb> queued" pill: the loop's next tick applies the intent, so
	// there is nothing further to render yet.
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderView(w, r, boardSwap)
}

// handleTicket edits one ticket's app-owned fields, branch and blocked_by. It writes an intent
// row and redirects, like every other write handler (inv. 9): the next tick's
// applyEditTicketIntents (loop.go) performs the actual write.
func (s *Server) handleTicket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ticketURL := r.FormValue("ticket")
	branch := r.FormValue("branch")
	if ticketURL == "" || branch == "" {
		http.Error(w, "ticket and branch are both required", http.StatusBadRequest)
		return
	}
	blockedBy := r.Form["blocked_by"]

	tickets, err := s.store.Tickets(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ticket, ok := ticketsByURL(tickets)[ticketURL]
	if !ok {
		http.Error(w, fmt.Sprintf("unknown ticket %q", ticketURL), http.StatusBadRequest)
		return
	}

	// Refused here rather than queued: applying it would leave the worktree and the row
	// disagreeing on the ticket's branch.
	if branch != ticket.Branch {
		obs, _, err := s.store.LastObservation(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if worktreePath, ok := obs.Worktrees[branchKey(ticket.Repo, ticket.Branch)]; ok {
			http.Error(w, fmt.Sprintf("branch %s already has a worktree at %s, remove it before changing branch",
				ticket.Branch, worktreePath), http.StatusConflict)
			return
		}
	}

	if err := s.store.QueueEditTicketIntent(ctx, ticketURL, branch, blockedBy, s.now()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type featureRow struct {
	Feature  string
	Imported bool
}

type importErrorView struct {
	Age     string
	Feature string
	Message string
}

type featuresPageView struct {
	chrome
	Features        []featureRow
	Query           string
	LastImportError *importErrorView
}

// handleFeatures lists every feature the configured repos' trackers offer, read fresh from the
// tracker on every request (§5, inv. 14).
func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	all, err := ImportFeatures(ctx, s.repos, s.trackerFor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tickets, err := s.store.Tickets(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lastErr, failed, err := s.store.LastImportError(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	imported := make(map[string]bool)
	for _, f := range distinctFeatures(tickets) {
		imported[f] = true
	}

	query := r.URL.Query().Get("q")
	q := strings.ToLower(query)
	rows := make([]featureRow, 0, len(all))
	for _, f := range all {
		if q != "" && !strings.Contains(strings.ToLower(f.Feature), q) {
			continue
		}
		rows = append(rows, featureRow{Feature: f.Feature, Imported: imported[f.Feature]})
	}

	chr, err := s.chromeFor(ctx, parseViewParams(url.Values{}))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	chr.Section = "features"

	view := featuresPageView{chrome: chr, Features: rows, Query: query}
	if failed {
		view.LastImportError = &importErrorView{
			Age: relative(s.now(), lastErr.At).Age, Feature: lastErr.Feature, Message: lastErr.Message,
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := featuresPage.Execute(w, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleFeatureRedirect scopes the board to {feature}: a feature has no id or slug of its own,
// only the tracker's own label name, which is exactly what ?feature= already matches.
func (s *Server) handleFeatureRedirect(w http.ResponseWriter, r *http.Request) {
	feature := r.PathValue("feature")
	http.Redirect(w, r, "/?feature="+url.QueryEscape(feature), http.StatusSeeOther)
}

func (s *Server) handleImportFeature(w http.ResponseWriter, r *http.Request) {
	feature := r.PathValue("feature")
	ctx := r.Context()
	if err := QueueImport(ctx, s.store, feature, s.now()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.nudge()

	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderView(w, r, boardSwap)
}

// randomGroup mints the token that ties every intent from one POST /launch call together —
// how N intents are recognised as one launch without a batch-key column.
func randomGroup() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate launch group: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func planTicketsByURL(tickets []Ticket) map[string]plan.Ticket {
	byURL := make(map[string]plan.Ticket, len(tickets))
	for _, t := range tickets {
		byURL[t.URL] = planTicket(t)
	}
	return byURL
}

// prsByBranch reads each ticket's own PR state back out under its own bare branch name, which is
// what internal/plan indexes by: plan.Ticket carries no repo-qualified key of its own, and this
// map's whole job is bridging Observation's repo-qualified storage back to plan's shape.
func prsByBranch(tickets []Ticket, obs Observation) map[string]plan.PRState {
	prs := make(map[string]plan.PRState, len(tickets))
	for _, t := range tickets {
		prs[t.Branch] = prState(obs.PRs[branchKey(t.Repo, t.Branch)].State)
	}
	return prs
}

func planTicket(t Ticket) plan.Ticket {
	return plan.Ticket{
		URL: t.URL, Repo: t.Repo, Branch: t.Branch, BlockedBy: t.BlockedBy,
	}
}

func prState(s gh.PRState) plan.PRState {
	switch s {
	case gh.Open:
		return plan.Open
	case gh.Merged:
		return plan.Merged
	case gh.Closed:
		return plan.Closed
	case gh.Absent:
		return plan.Absent
	default:
		return plan.Absent
	}
}

func workspaceName(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Base(dataDir)
}

func liveAgents(tickets []Ticket, obs Observation) int {
	live := 0
	for _, t := range tickets {
		if obs.Runs[t.URL].Alive {
			live++
		}
	}
	return live
}

func relative(now, then time.Time) ageView {
	return ageView{
		Age:   now.Sub(then).Round(time.Second).String() + " ago",
		Stamp: then.UTC().Format(time.RFC3339),
	}
}
