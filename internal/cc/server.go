package cc

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/verdict"
)

//go:embed page.tmpl
var pageSource string

var page = template.Must(template.New("page").Parse(pageSource))

// Server is the status page plus the launch-preview and launch-authorisation routes. It never
// writes the database directly except to queue a launch intent: every state it shows is
// derived from tasks and the last observation at render time (§5, inv. 14).
type Server struct {
	store            *Store
	now              func() time.Time
	stackingByRepo   map[string]bool
	checksByRepo     map[string]verdict.Predicate
	mergifySHAByRepo map[string]string
	mux              *http.ServeMux
}

// NewServer assembles the page and its routes over a store, a clock and the configured repos
// (stacking, the CI verdict predicate and the recorded mergify hash are all per-repo config,
// consulted on every render).
func NewServer(store *Store, now func() time.Time, repos []Repo) *Server {
	s := &Server{
		store: store, now: now,
		stackingByRepo: stackingByRepo(repos), checksByRepo: checksByRepo(repos),
		mergifySHAByRepo: mergifySHAByRepo(repos),
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
	Verbs  []string
	Branch string
	Base   string
	// BaseVerdict is the base's own CI verdict label ("review_me"/"needs_you"/"checking"/
	// "base_moved"), empty for a root row: a red check on a descendant whose base moved may not
	// be its own fault (plans/command-centre-phase-2.md § Phase 5).
	BaseVerdict string
	Worktree    string
	PR          string
	// Pgid, Elapsed and LogPath are plain, copy-pasteable text (docs/prds/prd-command-centre.md §
	// The page) — empty for a task with no run yet.
	Pgid        string
	Elapsed     string
	LogPath     string
	CancelCount int
}

type tickErrorView struct {
	Age     string
	Message string
}

type pageView struct {
	ObserveAge string
	LastError  *tickErrorView
	Rows       []row
	// LaunchVerb is how the template recognises the verb it renders as a checkbox in the
	// slice-wide launch form rather than as a per-row POST to /verb, without naming it in markup.
	LaunchVerb string
	CancelVerb string
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
	memberships, err := s.store.LaunchMemberships(ctx)
	if err != nil {
		return pageView{}, err
	}
	latestRuns, err := s.store.LatestRunsByTask(ctx)
	if err != nil {
		return pageView{}, err
	}
	pushFacts, err := s.store.PushFacts(ctx)
	if err != nil {
		return pageView{}, err
	}
	refreshFacts, err := s.store.RefreshFacts(ctx)
	if err != nil {
		return pageView{}, err
	}
	pushRows, err := s.store.LatestPushes(ctx)
	if err != nil {
		return pageView{}, err
	}
	checkingTicks, err := s.store.CheckingTicks(ctx)
	if err != nil {
		return pageView{}, err
	}

	now := s.now()
	vd := verdictDeps{
		pushRows: pushRows, checkingTicks: checkingTicks,
		checksByRepo: s.checksByRepo, mergifySHAByRepo: s.mergifySHAByRepo,
	}
	view := pageView{
		ObserveAge: "never",
		LaunchVerb: plan.VerbLaunch,
		CancelVerb: plan.VerbCancel,
		Rows: derive(
			tasks, obs, memberships, latestRuns, pushFacts, refreshFacts,
			vd, s.stackingByRepo, now,
		),
	}
	if observed {
		view.ObserveAge = age(now, obs.ObservedAt)
	}
	if failed {
		view.LastError = &tickErrorView{Age: age(now, lastErr.At), Message: lastErr.Message}
	}
	return view, nil
}

// derive computes every row's label from tasks plus the last observation. Nothing here is
// read from a stored status column, because there is not one. latestRuns and obs.Runs together
// are what let a task's state depend on its run: the store has the durable facts (pgid, log
// path, outcome), the observation has this tick's own liveness read — both survive a restart,
// so the page renders identically whether this is tick 1 or tick 4000 (§ Crash recovery).
// verdictDeps is the CI-verdict-specific facts derive needs per task, gathered once per render
// rather than threaded through as four more scalar parameters.
type verdictDeps struct {
	pushRows         map[string]PushRow
	checkingTicks    map[string]int
	checksByRepo     map[string]verdict.Predicate
	mergifySHAByRepo map[string]string
}

func derive(
	tasks []Task, obs Observation, memberships map[string]LaunchMembership,
	latestRuns map[string]RunSummary, pushFacts map[string]PushFact,
	refreshFacts map[string]RefreshFact, vd verdictDeps,
	stackingByRepo map[string]bool, now time.Time,
) []row {
	byURL := planTasksByURL(tasks)
	prs := prsByBranch(obs)

	rows := make([]row, 0, len(tasks))
	verdictLabelByBranch := make(map[string]string, len(tasks))
	for _, t := range tasks {
		pt := planTask(t)
		unlock := plan.Unlocked(pt, byURL, prs, stackingByRepo[t.Repo])
		runFact, pgid, elapsed, logPath := runFactFor(t, obs, latestRuns, pushFacts, refreshFacts, vd, now)
		membership := memberships[t.TicketURL]
		state, reason := plan.Status(plan.Facts{
			Task:            pt,
			Unlock:          unlock,
			Now:             now,
			Authorised:      membership.LaunchID != 0,
			LatestRun:       runFact,
			CancelledMember: membership.Cancelled,
		})
		verdictLabelByBranch[t.Branch] = verdictLabel(runFact)
		rows = append(rows, row{
			TicketURL:   t.TicketURL,
			State:       state.String(),
			Reason:      string(reason),
			Verbs:       plan.Verbs(state),
			Branch:      t.Branch,
			Base:        unlock.BaseBranch,
			Worktree:    obs.Worktrees[t.Branch],
			PR:          prSummary(obs.PRs[t.Branch]),
			Pgid:        pgid,
			Elapsed:     elapsed,
			LogPath:     logPath,
			CancelCount: membership.Members,
		})
	}

	// A second pass: a row's base is another row's own branch (never main, §4a), so its verdict
	// is only known once every row above has been built.
	for i := range rows {
		if rows[i].Base != "" && rows[i].Base != defaultBaseBranch {
			rows[i].BaseVerdict = verdictLabelByBranch[rows[i].Base]
		}
	}
	return rows
}

// runFactFor builds plan.Status's LatestRun input for one task, plus the plain-text pgid,
// elapsed time and log path the page renders alongside it. nil/empty when the task has no run.
// Push facts are only meaningful once the run's own outcome is push (docs/prds/prd-command-centre.md
// § Phase 4): PROpen reads this tick's own PR snapshot for the task's branch, never a stored
// column (inv. 14). The CI verdict, once PROpen and clear of a refusal or failure, is
// internal/verdict's own job (applyVerdict).
func runFactFor(
	t Task, obs Observation, latestRuns map[string]RunSummary,
	pushFacts map[string]PushFact, refreshFacts map[string]RefreshFact, vd verdictDeps, now time.Time,
) (runFact *plan.RunFact, pgid, elapsed, logPath string) {
	summary, ok := latestRuns[t.TicketURL]
	if !ok {
		return nil, "", "", ""
	}

	fact := &plan.RunFact{LogPath: summary.LogPath, Alive: obs.Runs[t.TicketURL].Alive}
	if summary.HasOutcome {
		fact.HasOutcome = true
		fact.Outcome = summary.Outcome
		if summary.Outcome == plan.OutcomePush {
			pf := pushFacts[t.TicketURL]
			fact.PushRefused = pf.Refused
			fact.PushRefusedPath = pf.RefusedPath
			fact.PushFailed = pf.Failed
			rf := refreshFacts[t.TicketURL]
			fact.RefreshRefused = rf.Refused
			fact.RefreshRefusedReason = plan.Reason(rf.Reason)
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
	stackedBase := pushRow.BaseBranch != "" && pushRow.BaseBranch != defaultBaseBranch
	mergifySHA := vd.mergifySHAByRepo[t.Repo]

	result := verdict.Evaluate(predicate, verdict.Input{
		Checks:       verdictChecks(pr.Checks),
		HeadOidMatch: pushRow.PushedTip != "" && pr.HeadOid == pushRow.PushedTip,
		StackedBase:  stackedBase,
		BaseSHAMatch: obs.BranchTips[pushRow.BaseBranch] == pushRow.BaseSHAAtPush,
		ConfigHashOK: mergifySHA == "" || obs.MergifyHash[t.Repo] == mergifySHA,
		PushedAt:     pushRow.PushedAt,
		Now:          pushRow.PushedAt.Add(time.Duration(vd.checkingTicks[t.TicketURL]) * tickPeriod),
		AuthorLogin:  pr.AuthorLogin,
	})

	switch result.Verdict {
	case verdict.ReviewMe:
		fact.VerdictReviewMe = true
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
	Hash      string
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
	memberships, err := s.store.LaunchMemberships(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rows := make([]previewRow, 0, len(requested))
	for _, ticketURL := range requested {
		t := byURL[ticketURL]
		stacking := s.stackingByRepo[t.Repo]
		unlock := plan.Unlocked(t, byURL, prs, stacking)
		label, reason := plan.Preview(unlock, slice, memberships[ticketURL].LaunchID)

		base := unlock.BaseBranch
		if base == "" {
			base = plan.ProspectiveBase(t, byURL, stacking)
		}
		rows = append(rows, previewRow{
			TicketURL: ticketURL,
			Label:     label.String(),
			Reason:    string(reason),
			Base:      "origin/" + base,
			Hash:      plan.Hash(plan.Compose(t, nil)),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rows); err != nil {
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

	tasks, err := s.store.Tasks(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	byURL := planTasksByURL(tasks)
	for _, ticketURL := range requested {
		if _, ok := byURL[ticketURL]; !ok {
			http.Error(w, fmt.Sprintf("unknown task %q", ticketURL), http.StatusBadRequest)
			return
		}
	}

	group, err := randomGroup()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := s.now()
	for _, ticketURL := range requested {
		hash := plan.Hash(plan.Compose(byURL[ticketURL], nil))
		if err := s.store.QueueLaunchIntent(ctx, ticketURL, hash, group, now); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
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
	w.WriteHeader(http.StatusAccepted)
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
	return plan.Task{TicketURL: t.TicketURL, Repo: t.Repo, Branch: t.Branch, BlockedBy: t.BlockedBy}
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
