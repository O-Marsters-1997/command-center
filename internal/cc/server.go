package cc

import (
	"cmp"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
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

//go:embed page.css
var pageCSS string

var page = template.Must(template.New("page").
	Funcs(template.FuncMap{
		"css":   func() template.CSS { return template.CSS(pageCSS) },
		"head":  func(r *row) rowSlot { return newRowSlot(*r, true, 0) },
		"child": func(r row, depth int) rowSlot { return newRowSlot(r, false, depth) },
	}).
	Parse(pageSource))

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
// derived from tasks and the last observation at render time (§5, inv. 14).
type Server struct {
	store             *Store
	now               func() time.Time
	stackingByRepo    map[string]bool
	checksByRepo      map[string]verdict.Predicate
	mergifySHAByRepo  map[string]string
	compatCheckByRepo map[string]string
	seams             []Seam
	repoPaths         map[string]string
	seamsRoot         string
	mux               *http.ServeMux
}

// NewServer assembles the page and its routes over a store, a clock, the configured repos and
// seams (stacking, the verdict predicate, the mergify hash, the compat check name and retirement
// pointers are all per-repo/-seam config) and the workspace root seams resolve against.
func NewServer(store *Store, now func() time.Time, repos []Repo, seams []Seam, seamsRoot string) *Server {
	s := &Server{
		store: store, now: now,
		stackingByRepo: stackingByRepo(repos), checksByRepo: checksByRepo(repos),
		mergifySHAByRepo: mergifySHAByRepo(repos), compatCheckByRepo: compatCheckByRepo(repos),
		seams: seams, repoPaths: repoPathsByName(seamsRoot, repos), seamsRoot: seamsRoot,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /preview", s.handlePreview)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("POST /launch", requireBrowserOrigin(s.handleLaunch))
	mux.HandleFunc("POST /verb", requireBrowserOrigin(s.handleVerb))
	s.mux = mux
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

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
	TicketURL string
	State     string
	Reason    string
	// Verbs comes from internal/plan: which verbs a state offers is a decision, so it is table-
	// tested beside plan.Status rather than spelled out per state in the template.
	Verbs        []string
	Branch       string
	PendingVerbs []string
	Base         string
	// SeamChanged is a flag, not a State (docs/designs/command-centre-design.md § 5): it
	// composes with State rather than replacing it.
	SeamChanged bool
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
	// The page) — empty for a task with no run yet.
	Pgid        string
	Elapsed     string
	LogPath     string
	CancelCount int
	// Draft mirrors the PR's own observed isDraft, not DraftGate's own opinion: a failed gh pr
	// ready leaves GitHub's real state unchanged, and this must still render honestly.
	Draft       bool
	DraftReason string
}

type tickErrorView struct {
	Age     string
	Message string
}

// group is one blocker and the rows waiting on it. A row with no blocker in the task set is its
// own group with a nil Root (docs/prds/prd-operator-surface.md § Reading the board).
type group struct {
	Root     *row
	Children []row
}

type pageView struct {
	ObserveAge string
	LastError  *tickErrorView
	Groups     []group
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	view, err := s.render(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := page.Execute(w, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) render(ctx context.Context) (pageView, error) {
	tasks, err := s.store.Tasks(ctx)
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
	facts, vd, err := s.loadTaskFacts(ctx)
	if err != nil {
		return pageView{}, err
	}

	now := s.now()
	view := pageView{
		ObserveAge: "never",
		Groups: groupRows(
			derive(ctx, tasks, obs, facts, vd, s.stackingByRepo, s.seams, s.repoPaths, s.seamsRoot, now)),
	}
	if observed {
		view.ObserveAge = age(now, obs.ObservedAt)
	}
	if failed {
		view.LastError = &tickErrorView{Age: age(now, lastErr.At), Message: lastErr.Message}
	}
	return view, nil
}

// loadTaskFacts gathers the taskFacts and verdictDeps both render and handlePreview need.
func (s *Server) loadTaskFacts(ctx context.Context) (taskFacts, verdictDeps, error) {
	memberships, err := s.store.LaunchMemberships(ctx)
	if err != nil {
		return taskFacts{}, verdictDeps{}, err
	}
	latestRuns, err := s.store.LatestRunsByTask(ctx)
	if err != nil {
		return taskFacts{}, verdictDeps{}, err
	}
	pushFacts, err := s.store.PushFacts(ctx)
	if err != nil {
		return taskFacts{}, verdictDeps{}, err
	}
	refreshFacts, err := s.store.RefreshFacts(ctx)
	if err != nil {
		return taskFacts{}, verdictDeps{}, err
	}
	pendingVerbs, err := s.store.PendingIntentsByTask(ctx)
	if err != nil {
		return taskFacts{}, verdictDeps{}, err
	}
	vd, err := verdictDepsFor(ctx, s.store, s.checksByRepo, s.mergifySHAByRepo, s.compatCheckByRepo)
	if err != nil {
		return taskFacts{}, verdictDeps{}, err
	}

	facts := taskFacts{
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

// taskFacts is the durable per-task state a row is derived from, keyed by ticket URL.
type taskFacts struct {
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
	ctx context.Context, tasks []Task, obs Observation, facts taskFacts, vd verdictDeps,
	stackingByRepo map[string]bool, seams []Seam, repoPaths map[string]string, seamsRoot string, now time.Time,
) []row {
	byURL := planTasksByURL(tasks)
	prs := prsByBranch(obs)
	retirements := retirementsByName(seams, byURL, prs, repoPaths)

	rows := make([]row, 0, len(tasks))
	verdictLabelByBranch := make(map[string]string, len(tasks))
	baseByBranch := make(map[string]string, len(tasks))
	for _, t := range tasks {
		pt := planTask(t)
		unlock := plan.Unlocked(pt, byURL, prs, stackingByRepo[t.Repo])
		runFact, pgid, elapsed, logPath := runFactFor(t, obs, facts, vd, now)
		membership := facts.memberships[t.TicketURL]
		latestRun, hasRun := facts.latestRuns[t.TicketURL]
		state, reason := plan.Status(plan.Facts{
			Task:            pt,
			Unlock:          unlock,
			Now:             now,
			Authorised:      membership.LaunchID != 0,
			LatestRun:       runFact,
			CancelledMember: membership.Cancelled,
		})
		verdictLabelByBranch[t.Branch] = verdictLabel(runFact)
		baseByBranch[t.Branch] = unlock.BaseBranch
		pr := obs.PRs[t.Branch]
		composed, _, composeOK := composePrompt(ctx, seamsRoot, pt, retirements)
		rows = append(rows, row{
			TicketURL:    t.TicketURL,
			State:        state.String(),
			Reason:       string(reason),
			Verbs:        plan.Verbs(state),
			PendingVerbs: facts.pendingVerbs[t.TicketURL],
			Branch:       t.Branch,
			Base:         unlock.BaseBranch,
			Worktree:     obs.Worktrees[t.Branch],
			PR:           prSummary(pr),
			Pgid:         pgid,
			Elapsed:      elapsed,
			LogPath:      logPath,
			CancelCount:  membership.Members,
			Warning:      readyToMergeWarning(pr),
			Blocking:     unlock.Blocking,
			Draft:        pr.IsDraft,
			DraftReason:  draftReasonFor(pr, pt, byURL, prs, runFact),
			SeamChanged: plan.SeamChanged(plan.SeamCheck{
				HasRun: hasRun, Authorised: membership.LaunchID != 0, ComposeOK: composeOK,
				ComposedHash: plan.Hash(composed), RunHash: latestRun.PromptHash, MemberHash: membership.PromptHash,
			}),
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

// groupRows keys a fan-in row's group on the first blocker in its own Blocking
// (internal/plan/plan.go:119); Reason still names every blocker.
func groupRows(rows []row) []group {
	byURL := make(map[string]row, len(rows))
	childrenByRoot := make(map[string][]row, len(rows))
	for _, r := range rows {
		byURL[r.TicketURL] = r
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
			return cmp.Compare(a.TicketURL, b.TicketURL)
		})
		rootRow := byURL[root]
		groups = append(groups, group{Root: &rootRow, Children: children})
	}

	var ungrouped []row
	for _, r := range rows {
		if len(r.Blocking) == 0 {
			if _, isRoot := childrenByRoot[r.TicketURL]; !isRoot {
				ungrouped = append(ungrouped, r)
			}
		}
	}
	slices.SortFunc(ungrouped, func(a, b row) int { return cmp.Compare(a.TicketURL, b.TicketURL) })
	for _, r := range ungrouped {
		groups = append(groups, group{Children: []row{r}})
	}
	return groups
}

// baseVerdict is the preview's read of a stacked row's base before authorising: empty for main,
// otherwise the base task's own CI verdict, exactly what the main page shows once the row has
// launched (docs/designs/command-centre-design.md § 4b, "you are about to build on a red parent").
func baseVerdict(
	base string, tasksByBranch map[string]Task, obs Observation, facts taskFacts, vd verdictDeps, now time.Time,
) string {
	if base == "" || base == defaultBaseBranch {
		return ""
	}
	baseTask, ok := tasksByBranch[base]
	if !ok {
		return ""
	}
	runFact, _, _, _ := runFactFor(baseTask, obs, facts, vd, now)
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
	pr gh.PR, t plan.Task, byURL map[string]plan.Task, prs map[string]plan.PRState, runFact *plan.RunFact,
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

// runFactFor builds plan.Status's LatestRun input for one task, plus the pgid, elapsed time and
// log path the page renders. Push facts only count once the run's outcome is push, and PROpen
// reads this tick's PR snapshot rather than a stored column (inv. 14).
func runFactFor(
	t Task, obs Observation, facts taskFacts, vd verdictDeps, now time.Time,
) (runFact *plan.RunFact, pgid, elapsed, logPath string) {
	summary, ok := facts.latestRuns[t.TicketURL]
	if !ok {
		return nil, "", "", ""
	}

	fact := &plan.RunFact{LogPath: summary.LogPath, Alive: obs.Runs[t.TicketURL].Alive}
	if summary.HasOutcome {
		fact.HasOutcome = true
		fact.Outcome = summary.Outcome
		if summary.Outcome == plan.OutcomePush {
			pf := facts.pushes[t.TicketURL]
			fact.PushRefused = pf.Refused
			fact.PushRefusedPath = pf.RefusedPath
			fact.PushFailed = pf.Failed
			rf := facts.refreshes[t.TicketURL]
			fact.RefreshRefused = rf.Refused
			fact.RefreshRefusedReason = plan.Reason(rf.Reason)
			fact.MidMerge = obs.MidMerge[t.Branch]
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
// "main", unlike a task's own branch name, so the plain name would collide the moment a second
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
func applyVerdict(fact *plan.RunFact, t Task, obs Observation, vd verdictDeps) {
	predicate := vd.checksByRepo[t.Repo]
	if predicate.IsZero() {
		return
	}
	pushRow, pushed := vd.pushRows[t.TicketURL]
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
		Now:          pushRow.PushedAt.Add(time.Duration(vd.checkingTicks[t.TicketURL]) * tickPeriod),
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

// previewRow is one line of a launch preview: what would happen to this task, and why. Base
// carries the literal origin/ prefix tp new and gh pr create --base need — deliberately
// different from the main page's Base column, which has no such prefix.
type previewRow struct {
	TicketURL string
	Label     string
	Reason    string
	Base      string
	// Refused is Label's own refused case, so the template renders the row without a checkbox
	// rather than comparing the label string it prints.
	Refused bool
	// BaseVerdict is the base's own CI verdict where the base is a blocker's branch, empty for
	// a root row — "you are about to build on a red parent" is read before authorising, not
	// after (docs/designs/command-centre-design.md § 4b).
	BaseVerdict string
	Hash        string
	// Prompt is the fully composed prompt this launch would authorise — the implement
	// instruction plus every seam file's content, in config order — empty when a named seam
	// had no readable content, which is itself what refuses the row.
	Prompt string
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requested := r.URL.Query()["task"]
	if len(requested) == 0 {
		http.Error(w, "at least one ?task= is required", http.StatusBadRequest)
		return
	}

	tasks, err := s.store.Tasks(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	byURL := planTasksByURL(tasks)
	tasksByBranch := make(map[string]Task, len(tasks))
	for _, t := range tasks {
		tasksByBranch[t.Branch] = t
	}

	slice := make(map[string]bool, len(requested))
	for _, ticketURL := range requested {
		if _, ok := byURL[ticketURL]; !ok {
			http.Error(w, fmt.Sprintf("unknown task %q", ticketURL), http.StatusBadRequest)
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
	retirements := retirementsByName(s.seams, byURL, prs, s.repoPaths)
	facts, vd, err := s.loadTaskFacts(ctx)
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
		label, reason := plan.Preview(unlock, slice, facts.memberships[ticketURL].LaunchID)

		base := unlock.BaseBranch
		if base == "" {
			base = plan.ProspectiveBase(t, byURL, stacking)
		}
		row := previewRow{
			TicketURL:   ticketURL,
			Label:       label.String(),
			Reason:      string(reason),
			Base:        "origin/" + base,
			BaseVerdict: baseVerdict(base, tasksByBranch, obs, facts, vd, now),
		}
		if composed, refused, ok := composePrompt(ctx, s.seamsRoot, t, retirements); ok {
			row.Hash = plan.Hash(composed)
			row.Prompt = composed
		} else if label != plan.Refused {
			label = plan.Refused
			row.Label = label.String()
			row.Reason = fmt.Sprintf("%q has no readable content", refused)
		}
		row.Refused = label == plan.Refused
		rows = append(rows, row)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := previewPage.Execute(w, rows); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleLaunch queues one launch intent per requested task, all sharing one fresh group token
// so the next tick's ApplyLaunchIntents recognises them as a single authorisation. It does not
// re-check Preview's Refused case: an authorised task whose blocker sits outside the slice
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
		http.Error(w, "at least one task is required", http.StatusBadRequest)
		return
	}
	// One `hash` field per launchable row, each naming its own task: an unchecked row still posts
	// its hidden hash, so pairing by position would pair the survivors wrong.
	previewed := make(map[string]string, len(r.Form["hash"]))
	for _, field := range r.Form["hash"] {
		ticketURL, hash, ok := strings.Cut(field, " ")
		if !ok {
			http.Error(w, fmt.Sprintf("malformed hash field %q, want \"<task> <hash>\"", field),
				http.StatusBadRequest)
			return
		}
		previewed[ticketURL] = hash
	}

	tasks, err := s.store.Tasks(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	byURL := planTasksByURL(tasks)
	obs, _, err := s.store.LastObservation(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	retirements := retirementsByName(s.seams, byURL, prsByBranch(obs), s.repoPaths)

	hashes := make(map[string]string, len(requested))
	for _, ticketURL := range requested {
		t, ok := byURL[ticketURL]
		if !ok {
			http.Error(w, fmt.Sprintf("unknown task %q", ticketURL), http.StatusBadRequest)
			return
		}
		composed, refused, ok := composePrompt(ctx, s.seamsRoot, t, retirements)
		if !ok {
			http.Error(w, fmt.Sprintf("task %s names %q with no readable content", ticketURL, refused),
				http.StatusBadRequest)
			return
		}
		hash := plan.Hash(composed)
		if want, ok := previewed[ticketURL]; ok && want != hash {
			http.Error(w, fmt.Sprintf("task %s was previewed at hash %s and now composes to %s",
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

// handleVerb queues one verb intent against one task — a handler only ever does this single
// blind INSERT; the loop is the sole reader and actor on it (inv. 9, see loop.go's
// applyKillIntents, push.go's applyRetryPushIntents and verbs.go's re-run/close-pr/
// remove-worktree appliers). `cancel` is Phase 2 and is not implemented.
func (s *Server) handleVerb(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// FormValue, not URL.Query: the page's per-row form posts both fields in the body
	// (plans/command-centre-phase-1.md § Routes). It reads the query string too, which is what
	// keeps a hand-built `POST /verb?verb=kill&task=...` working unchanged.
	verb := r.FormValue("verb")
	taskURL := r.FormValue("task")
	if verb == "" || taskURL == "" {
		http.Error(w, "verb and task are both required", http.StatusBadRequest)
		return
	}
	if !supportedVerbs[verb] {
		http.Error(w, fmt.Sprintf("unsupported verb %q", verb), http.StatusBadRequest)
		return
	}

	tasks, err := s.store.Tasks(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, ok := planTasksByURL(tasks)[taskURL]; !ok {
		http.Error(w, fmt.Sprintf("unknown task %q", taskURL), http.StatusBadRequest)
		return
	}

	if err := s.store.QueueVerbIntent(ctx, taskURL, verb, s.now()); err != nil {
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

func planTasksByURL(tasks []Task) map[string]plan.Task {
	byURL := make(map[string]plan.Task, len(tasks))
	for _, t := range tasks {
		byURL[t.TicketURL] = planTask(t)
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

func planTask(t Task) plan.Task {
	return plan.Task{
		TicketURL: t.TicketURL, Repo: t.Repo, Branch: t.Branch, BlockedBy: t.BlockedBy, Seams: t.Seams,
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

func age(now, then time.Time) string {
	return now.Sub(then).Round(time.Second).String() + " ago"
}
