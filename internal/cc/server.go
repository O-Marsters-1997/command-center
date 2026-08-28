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
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

//go:embed page.tmpl
var pageSource string

//go:embed assets all:assets/dist
var assetsDir embed.FS

var page = template.Must(template.New("page").
	Funcs(template.FuncMap{
		"head":        func(r *row) rowSlot { return newRowSlot(*r, true, 0) },
		"child":       func(r row, depth int) rowSlot { return newRowSlot(r, false, depth) },
		"destructive": func(verb string) bool { _, ok := destructiveVerbs[verb]; return ok },
	}).
	Parse(pageSource))

//go:embed board.tmpl
var boardSource string

var boardFragment = template.Must(page.New("board").Parse(boardSource))

// rowSlot carries LaunchVerb and CancelVerb because html/template resets $ to the invoked
// subtemplate's own argument, so the "row" subtemplate cannot see pageView's copies.
type rowSlot struct {
	row
	Head       bool
	Depth      int
	LaunchVerb string
	CancelVerb string
}

func newRowSlot(r row, head bool, depth int) rowSlot {
	return rowSlot{row: r, Head: head, Depth: depth, LaunchVerb: plan.VerbLaunch, CancelVerb: plan.VerbCancel}
}

//go:embed preview.tmpl
var previewSource string

var previewPage = template.Must(template.New("preview").Parse(previewSource))

// Server is the status page plus the launch-preview and launch-authorisation routes. It never
// writes the database directly except to queue a launch intent: every state it shows is
// derived from tickets and the last observation at render time (§5, inv. 14).
type Server struct {
	store             *Store
	now               func() time.Time
	stackingByRepo    map[string]bool
	checksByRepo      map[string]verdict.Predicate
	mergifySHAByRepo  map[string]string
	compatCheckByRepo map[string]string
	dataDir           string
	mux               *http.ServeMux
}

// NewServer assembles the page and its routes over a store, a clock, the configured repos and
// the data directory: stacking, the verdict predicate, the mergify hash and the compat check
// name are all per-repo config, and dataDir is the fleet the header names.
func NewServer(store *Store, now func() time.Time, repos []Repo, dataDir string) *Server {
	s := &Server{
		store: store, now: now, dataDir: dataDir,
		stackingByRepo: stackingByRepo(repos), checksByRepo: checksByRepo(repos),
		mergifySHAByRepo: mergifySHAByRepo(repos), compatCheckByRepo: compatCheckByRepo(repos),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /board", s.handleBoard)
	mux.Handle("GET /assets/", http.FileServerFS(assetsDir))
	mux.HandleFunc("GET /assets/app.css", s.handleStylesheet)
	mux.HandleFunc("GET /ticket/{ticket}/detail", s.handleDetail)
	mux.HandleFunc("GET /ticket/{ticket}/log", s.handleLog)
	mux.HandleFunc("GET /preview", s.handlePreview)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("GET /confirm", s.handleConfirm)
	mux.HandleFunc("POST /launch", requireBrowserOrigin(s.handleLaunch))
	mux.HandleFunc("POST /verb", requireBrowserOrigin(s.handleVerb))
	s.mux = mux
	return s
}

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

type row struct {
	URL string
	// Title is empty when the tick's read did not cover the ticket: a fresh DB, or an issue past
	// `gh issue list`'s own 100-row limit.
	Title  string
	State  string
	Reason string
	// Verbs comes from internal/plan: which verbs a state offers is a decision, so it is table-
	// tested beside plan.Status rather than spelled out per state in the template.
	Verbs        []string
	Branch       string
	PendingVerbs []string
	Base         string
	// BaseVerdict is the base's own CI verdict label ("review_me"/"needs_you"/"checking"/
	// "base_moved"), empty for a root row: a red check on a descendant whose base moved may not
	// be its own fault (plans/command-centre-phase-2.md § Phase 5).
	BaseVerdict string
	// StackDepth and MergeOrder are the row's distance from a root and the order it merges in,
	// bottom-up — the app never merges a PR, so this is the only place the order is shown
	// (docs/prds/prd-command-centre.md § The page → stack order).
	StackDepth int
	MergeOrder int
	// Warning is invariant 2's hazard, named on the row: a non-main-based PR carrying
	// ready-to-merge would squash-merge into its parent branch with the parent's own checks
	// unseen, and empty otherwise. The app never applies that label itself.
	Warning  string
	Blocking []string
	Worktree string
	PR       string
	// Pgid, Elapsed and LogPath are plain, copy-pasteable text (docs/prds/prd-command-centre.md §
	// The page) — empty for a ticket with no run yet.
	Pgid        string
	Elapsed     string
	LogPath     string
	CancelCount int
	// BaselineSHA and Checks are the detail fragment's, not the board's: the row is derived once
	// and every island reads it (docs/prds/prd-operator-surface.md § One derivation).
	BaselineSHA string
	Checks      []check
	// Draft mirrors the PR's own observed isDraft, not DraftGate's own opinion: a failed gh pr
	// ready leaves GitHub's real state unchanged, and this must still render honestly.
	Draft       bool
	DraftReason string
}

type check struct {
	Name       string
	Status     string
	Conclusion string
}

func (r row) Ticket() string { return "#" + path.Base(r.URL) }

// DetailID is the row's stable DOM id. A ticket URL is neither a usable id nor a CSS selector,
// and htmx needs both: hx-target resolves the selector, and hx-preserve matches the id to keep
// an expanded detail mounted through the board's own five-second swap.
func (r row) DetailID() string {
	sum := sha256.Sum256([]byte(r.URL))
	return "detail-" + hex.EncodeToString(sum[:6])
}

// DetailPath percent-encodes the ticket URL into one path segment, "://" and all.
func (r row) DetailPath() string { return "/ticket/" + url.PathEscape(r.URL) + "/detail" }

type tickErrorView struct {
	Age     string
	Message string
}

// group is one blocker and the rows waiting on it. A row with no blocker in the ticket set is its
// own group with a nil Root (docs/prds/prd-operator-surface.md § Reading the board).
type group struct {
	Root     *row
	Children []row
}

type pageView struct {
	Workspace  string
	LiveAgents int
	ObserveAge string
	// ObserveStale is decided here rather than in the template, which cannot compare durations.
	ObserveStale bool
	LastError    *tickErrorView
	Groups       []group
}

const observeStaleAfter = 20 * time.Second

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.renderView(w, r, page)
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	s.renderView(w, r, boardFragment)
}

func (s *Server) renderView(w http.ResponseWriter, r *http.Request, tmpl *template.Template) {
	view, err := s.render(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) render(ctx context.Context) (pageView, error) {
	tickets, err := s.store.Tickets(ctx)
	if err != nil {
		return pageView{}, err
	}
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
	view := pageView{
		Workspace:    workspaceName(s.dataDir),
		LiveAgents:   liveAgents(tickets, obs),
		ObserveAge:   "never",
		ObserveStale: true,
		Groups:       groupRows(rows),
	}
	if observed {
		view.ObserveAge = age(now, obs.ObservedAt)
		view.ObserveStale = now.Sub(obs.ObservedAt) >= observeStaleAfter
	}
	if failed && (!observed || lastErr.At.After(obs.ObservedAt)) {
		view.LastError = &tickErrorView{Age: age(now, lastErr.At), Message: lastErr.Message}
	}
	return view, nil
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
	vd, err := verdictDepsFor(ctx, s.store, s.checksByRepo, s.mergifySHAByRepo, s.compatCheckByRepo)
	if err != nil {
		return ticketFacts{}, verdictDeps{}, err
	}

	facts := ticketFacts{
		memberships: memberships, latestRuns: latestRuns,
		pushes: pushFacts, refreshes: refreshFacts, pendingVerbs: pendingVerbs,
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
}

// derive labels every row from the stored facts plus this tick's observation. No status is
// stored: facts are stored, labels are derived every tick
// (docs/designs/command-centre-design.md § Schema, inv. 14).
func derive(
	tickets []Ticket, obs Observation, facts ticketFacts, vd verdictDeps,
	stackingByRepo map[string]bool, now time.Time,
) []row {
	byURL := planTicketsByURL(tickets)
	prs := prsByBranch(obs)

	rows := make([]row, 0, len(tickets))
	verdictLabelByBranch := make(map[string]string, len(tickets))
	baseByBranch := make(map[string]string, len(tickets))
	for _, t := range tickets {
		pt := planTicket(t)
		unlock := plan.Unlocked(pt, byURL, prs, stackingByRepo[t.Repo])
		runFact, pgid, elapsed, logPath := runFactFor(t, obs, facts, vd, now)
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
		pr := obs.PRs[t.Branch]
		rows = append(rows, row{
			URL:          t.URL,
			Title:        obs.Titles[t.URL],
			State:        state.String(),
			Reason:       string(reason),
			Verbs:        plan.Verbs(state),
			PendingVerbs: facts.pendingVerbs[t.URL],
			Branch:       t.Branch,
			Base:         unlock.BaseBranch,
			Worktree:     obs.Worktrees[t.Branch],
			PR:           prSummary(pr),
			Pgid:         pgid,
			Elapsed:      elapsed,
			LogPath:      logPath,
			CancelCount:  membership.Members,
			Warning:      readyToMergeWarning(pr),
			BaselineSHA:  latestRun.BaselineSHA,
			Checks:       sortedChecks(pr.Checks),
			Blocking:     unlock.Blocking,
			Draft:        pr.IsDraft,
			DraftReason:  draftReasonFor(pr, pt, byURL, prs, runFact),
		})
	}

	// A second pass: a row's base is another row's own branch (never main, §4a), so its verdict
	// and its depth are only known once every row above has been built.
	for i := range rows {
		if rows[i].Base != "" && rows[i].Base != defaultBaseBranch {
			rows[i].BaseVerdict = verdictLabelByBranch[rows[i].Base]
		}
		rows[i].StackDepth = plan.StackDepth(rows[i].Branch, baseByBranch)
		rows[i].MergeOrder = rows[i].StackDepth + 1
	}
	return rows
}

func sortedChecks(checks map[string]gh.CheckState) []check {
	out := make([]check, 0, len(checks))
	for _, name := range slices.Sorted(maps.Keys(checks)) {
		out = append(out, check{Name: name, Status: checks[name].Status, Conclusion: checks[name].Conclusion})
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

	rootURLs := make([]string, 0, len(childrenByRoot))
	for root := range childrenByRoot {
		rootURLs = append(rootURLs, root)
	}
	slices.Sort(rootURLs)

	groups := make([]group, 0, len(rootURLs)+len(rows))
	for _, root := range rootURLs {
		children := childrenByRoot[root]
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
	runFact, _, _, _ := runFactFor(baseTicket, obs, facts, vd, now)
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
	t Ticket, obs Observation, facts ticketFacts, vd verdictDeps, now time.Time,
) (runFact *plan.RunFact, pgid, elapsed, logPath string) {
	summary, ok := facts.latestRuns[t.URL]
	if !ok {
		return nil, "", "", ""
	}

	fact := &plan.RunFact{LogPath: summary.LogPath, Alive: obs.Runs[t.URL].Alive}
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
			fact.MidMerge = obs.MidMerge[t.Branch]
			if obs.ConflictsWithBase[t.Branch] {
				fact.ConflictsWithMain = true
				fact.ConflictsWithMainReason = plan.Reason(
					fmt.Sprintf("%s no longer merges cleanly into main", t.Branch))
			}
			ownState := obs.PRs[t.Branch].State
			fact.PROpen = ownState == gh.Open
			fact.PRMerged = ownState == gh.Merged
			fact.PRClosedUnmerged = ownState == gh.Closed
			if fact.PROpen && !fact.PushRefused && !fact.PushFailed {
				applyVerdict(fact, t, obs, vd)
			}
		}
	}

	logPath = summary.LogPath
	if summary.Pgid != nil {
		pgid = strconv.Itoa(*summary.Pgid)
	}
	if fact.Alive && summary.ProcStartedAt != nil {
		elapsed = now.Sub(*summary.ProcStartedAt).Round(time.Second).String()
	}
	return fact, pgid, elapsed, logPath
}

// defaultBaseBranch mirrors internal/plan's own unexported copy: verdict's import guard (like
// plan's) forbids depending on that package for one string constant.
const defaultBaseBranch = "main"

// mainTipKey names defaultBaseBranch's own tip in Observation.BranchTips. Every repo has a
// "main", unlike a ticket's own branch name, so the plain name would collide the moment a second
// repo is configured; "//" can never appear in a real git branch name, so this key never can.
func mainTipKey(repo string) string { return repo + "//" + defaultBaseBranch }

// baseTipKey is the Observation.BranchTips key a recorded base resolves to: mainTipKey when it's
// main, the plain branch name otherwise (issue #85: main's own tip is checked exactly like a
// still-stacked base's, not exempted, since retargetOne can re-point a row onto it).
func baseTipKey(repo, base string) string {
	if base == defaultBaseBranch {
		return mainTipKey(repo)
	}
	return base
}

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

	pr := obs.PRs[t.Branch]
	hasRecordedBase := pushRow.BaseBranch != ""
	mergifySHA := vd.mergifySHAByRepo[t.Repo]

	result := verdict.Evaluate(predicate, verdict.Input{
		Checks:       verdictChecks(pr.Checks),
		HeadOidMatch: pushRow.PushedTip != "" && pr.HeadOid == pushRow.PushedTip,
		StackedBase:  hasRecordedBase,
		BaseSHAMatch: obs.BranchTips[baseTipKey(t.Repo, pushRow.BaseBranch)] == pushRow.BaseSHAAtPush,
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
		fact.VerdictNeedsYou = true
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

// previewRow is one line of a launch preview: what would happen to this ticket, and why. Base
// carries the literal origin/ prefix tp new and gh pr create --base need — deliberately
// different from the main page's Base column, which has no such prefix.
type previewRow struct {
	URL    string
	Label  string
	Reason string
	Base   string
	// Refused is Label's own refused case, so the template renders the row without a checkbox
	// rather than comparing the label string it prints.
	Refused bool
	// BaseVerdict is the base's own CI verdict where the base is a blocker's branch, empty for
	// a root row — "you are about to build on a red parent" is read before authorising, not
	// after (docs/designs/command-centre-design.md § 4b).
	BaseVerdict string
	Hash        string
	// Prompt is the fully composed prompt this launch would authorise.
	Prompt string
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requested := r.URL.Query()["task"]
	if len(requested) == 0 {
		http.Error(w, "at least one ?task= is required", http.StatusBadRequest)
		return
	}

	tickets, err := s.store.Tickets(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	byURL := planTicketsByURL(tickets)
	ticketsByBranch := make(map[string]Ticket, len(tickets))
	for _, t := range tickets {
		ticketsByBranch[t.Branch] = t
	}

	slice := make(map[string]bool, len(requested))
	for _, ticketURL := range requested {
		if _, ok := byURL[ticketURL]; !ok {
			http.Error(w, fmt.Sprintf("unknown ticket %q", ticketURL), http.StatusBadRequest)
			return
		}
		slice[ticketURL] = true
	}

	obs, _, err := s.store.LastObservation(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	prs := prsByBranch(obs)
	facts, vd, err := s.loadTicketFacts(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := s.now()

	rows := make([]previewRow, 0, len(requested))
	for _, ticketURL := range requested {
		t := byURL[ticketURL]
		stacking := s.stackingByRepo[t.Repo]
		unlock := plan.Unlocked(t, byURL, prs, stacking)
		label, reason := plan.Preview(
			unlock, slice, facts.memberships[ticketURL].LaunchID,
			conflictedBase(t, byURL, unlock, stacking, obs))

		base := unlock.BaseBranch
		if base == "" {
			base = plan.ProspectiveBase(t, byURL, stacking)
		}
		row := previewRow{
			URL:         ticketURL,
			Label:       label.String(),
			Reason:      string(reason),
			Base:        "origin/" + base,
			BaseVerdict: baseVerdict(base, ticketsByBranch, obs, facts, vd, now),
		}
		composed := plan.Compose(t)
		row.Hash = plan.Hash(composed)
		row.Prompt = composed
		row.Refused = label == plan.Refused
		rows = append(rows, row)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := previewPage.Execute(w, rows); err != nil {
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
	// and a hand-built `POST /launch?task=...&task=...` are the same repeated field to r.Form.
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	requested := r.Form["task"]
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
// remove-worktree appliers). `cancel` is Phase 2 and is not implemented.
func (s *Server) handleVerb(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// FormValue, not URL.Query: the page's per-row form posts both fields in the body
	// (plans/command-centre-phase-1.md § Routes). It reads the query string too, which is what
	// keeps a hand-built `POST /verb?verb=kill&task=...` working unchanged.
	verb := r.FormValue("verb")
	ticketURL := r.FormValue("task")
	if verb == "" || ticketURL == "" {
		http.Error(w, "verb and ticket are both required", http.StatusBadRequest)
		return
	}
	if !supportedVerbs[verb] {
		http.Error(w, fmt.Sprintf("unsupported verb %q", verb), http.StatusBadRequest)
		return
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

	if err := s.store.QueueVerbIntent(ctx, ticketURL, verb, s.now()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
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

func prsByBranch(obs Observation) map[string]plan.PRState {
	prs := make(map[string]plan.PRState, len(obs.PRs))
	for branch, pr := range obs.PRs {
		prs[branch] = prState(pr.State)
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

func prSummary(pr gh.PR) string {
	if pr.Number == 0 {
		return "none"
	}
	return fmt.Sprintf("#%d %s", pr.Number, pr.State)
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

func age(now, then time.Time) string {
	return now.Sub(then).Round(time.Second).String() + " ago"
}
