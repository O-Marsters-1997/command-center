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
)

//go:embed page.tmpl
var pageSource string

var page = template.Must(template.New("page").Parse(pageSource))

// Server is the status page plus the launch-preview and launch-authorisation routes. It never
// writes the database directly except to queue a launch intent: every state it shows is
// derived from tasks and the last observation at render time (§5, inv. 14).
type Server struct {
	store          *Store
	now            func() time.Time
	stackingByRepo map[string]bool
	mux            *http.ServeMux
}

// NewServer assembles the page and its routes over a store, a clock and the configured repos
// (stacking is per-repo config, consulted on every unlock decision).
func NewServer(store *Store, now func() time.Time, repos []Repo) *Server {
	s := &Server{store: store, now: now, stackingByRepo: stackingByRepo(repos)}
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
	Branch    string
	Base      string
	Worktree  string
	PR        string
	// Pgid, Elapsed and LogPath are plain, copy-pasteable text (docs/prd-command-centre.md §
	// The page) — empty for a task with no run yet.
	Pgid    string
	Elapsed string
	LogPath string
}

type tickErrorView struct {
	Age     string
	Message string
}

type pageView struct {
	ObserveAge string
	LastError  *tickErrorView
	Rows       []row
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
	authorised, err := s.store.ActiveMemberships(ctx)
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

	now := s.now()
	view := pageView{
		ObserveAge: "never",
		Rows:       derive(tasks, obs, authorised, latestRuns, pushFacts, s.stackingByRepo, now),
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
func derive(
	tasks []Task, obs Observation, authorised map[string]bool, latestRuns map[string]RunSummary,
	pushFacts map[string]PushFact, stackingByRepo map[string]bool, now time.Time,
) []row {
	byURL := planTasksByURL(tasks)
	prs := prsByBranch(obs)

	rows := make([]row, 0, len(tasks))
	for _, t := range tasks {
		pt := planTask(t)
		unlock := plan.Unlocked(pt, byURL, prs, stackingByRepo[t.Repo])
		runFact, pgid, elapsed, logPath := runFactFor(t.TicketURL, t.Branch, obs, latestRuns, pushFacts, now)
		state, reason := plan.Status(plan.Facts{
			Task:       pt,
			Unlock:     unlock,
			Now:        now,
			Authorised: authorised[t.TicketURL],
			LatestRun:  runFact,
		})
		rows = append(rows, row{
			TicketURL: t.TicketURL,
			State:     state.String(),
			Reason:    string(reason),
			Branch:    t.Branch,
			Base:      unlock.BaseBranch,
			Worktree:  obs.Worktrees[t.Branch],
			PR:        prSummary(obs.PRs[t.Branch]),
			Pgid:      pgid,
			Elapsed:   elapsed,
			LogPath:   logPath,
		})
	}
	return rows
}

// runFactFor builds plan.Status's LatestRun input for one task, plus the plain-text pgid,
// elapsed time and log path the page renders alongside it. nil/empty when the task has no run.
// Push facts are only meaningful once the run's own outcome is push (docs/prd-command-centre.md
// § Phase 4): PROpen reads this tick's own PR snapshot for the task's branch, never a stored
// column (inv. 14).
func runFactFor(
	ticketURL, branch string, obs Observation, latestRuns map[string]RunSummary,
	pushFacts map[string]PushFact, now time.Time,
) (runFact *plan.RunFact, pgid, elapsed, logPath string) {
	summary, ok := latestRuns[ticketURL]
	if !ok {
		return nil, "", "", ""
	}

	fact := &plan.RunFact{LogPath: summary.LogPath, Alive: obs.Runs[ticketURL].Alive}
	if summary.HasOutcome {
		fact.HasOutcome = true
		fact.Outcome = summary.Outcome
		if summary.Outcome == plan.OutcomePush {
			pf := pushFacts[ticketURL]
			fact.PushRefused = pf.Refused
			fact.PushRefusedPath = pf.RefusedPath
			fact.PushFailed = pf.Failed
			fact.PROpen = obs.PRs[branch].State == gh.Open
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

// handleEvents dumps the append-only audit log as JSON: what reconstructs the whole run, every
// authorisation, launch, disposition, push and refusal (docs/prd-command-centre.md § Phase 4).
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

	rows := make([]previewRow, 0, len(requested))
	for _, ticketURL := range requested {
		t := byURL[ticketURL]
		stacking := s.stackingByRepo[t.Repo]
		unlock := plan.Unlocked(t, byURL, prs, stacking)
		label, reason := plan.Preview(unlock, slice)

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
// applyKillIntents and push.go's applyRetryPushIntents). `kill` and `retry-push` are the verbs
// this phase implements; the design's route table also lists re-run, close-pr and
// remove-worktree for later phases.
func (s *Server) handleVerb(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	verb := r.URL.Query().Get("verb")
	taskURL := r.URL.Query().Get("task")
	if verb == "" || taskURL == "" {
		http.Error(w, "verb and task are both required", http.StatusBadRequest)
		return
	}
	if verb != killVerb && verb != retryPushVerb {
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
