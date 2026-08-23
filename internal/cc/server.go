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
	stacking := make(map[string]bool, len(repos))
	for _, r := range repos {
		stacking[r.Name] = r.Stacking
	}

	s := &Server{store: store, now: now, stackingByRepo: stacking}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /preview", s.handlePreview)
	mux.HandleFunc("POST /launch", requireBrowserOrigin(s.handleLaunch))
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

	now := s.now()
	view := pageView{ObserveAge: "never", Rows: derive(tasks, obs, authorised, s.stackingByRepo, now)}
	if observed {
		view.ObserveAge = age(now, obs.ObservedAt)
	}
	if failed {
		view.LastError = &tickErrorView{Age: age(now, lastErr.At), Message: lastErr.Message}
	}
	return view, nil
}

// derive computes every row's label from tasks plus the last observation. Nothing here is
// read from a stored status column, because there is not one.
func derive(
	tasks []Task, obs Observation, authorised, stackingByRepo map[string]bool, now time.Time,
) []row {
	byURL := planTasksByURL(tasks)
	prs := prsByBranch(obs)

	rows := make([]row, 0, len(tasks))
	for _, t := range tasks {
		pt := planTask(t)
		unlock := plan.Unlocked(pt, byURL, prs, stackingByRepo[t.Repo])
		state, reason := plan.Status(plan.Facts{
			Task:       pt,
			Unlock:     unlock,
			Now:        now,
			Authorised: authorised[t.TicketURL],
		})
		rows = append(rows, row{
			TicketURL: t.TicketURL,
			State:     state.String(),
			Reason:    string(reason),
			Branch:    t.Branch,
			Base:      unlock.BaseBranch,
			Worktree:  obs.Worktrees[t.Branch],
			PR:        prSummary(obs.PRs[t.Branch]),
		})
	}
	return rows
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
