package cc

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

//go:embed page.tmpl
var pageSource string

var page = template.Must(template.New("page").Parse(pageSource))

// Server is the status page. It never writes the database: every state it shows is derived
// from tasks and the last observation at render time (§5, inv. 14).
type Server struct {
	store *Store
	now   func() time.Time
}

// NewServer assembles the page over a store and a clock.
func NewServer(store *Store, now func() time.Time) *Server {
	return &Server{store: store, now: now}
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

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "only GET", http.StatusMethodNotAllowed)
		return
	}

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

	now := s.now()
	view := pageView{ObserveAge: "never", Rows: derive(tasks, obs, now)}
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
func derive(tasks []Task, obs Observation, now time.Time) []row {
	byURL := make(map[string]plan.Task, len(tasks))
	for _, t := range tasks {
		byURL[t.TicketURL] = planTask(t)
	}
	prs := make(map[string]plan.PRState, len(obs.PRs))
	for branch, pr := range obs.PRs {
		prs[branch] = prState(pr.State)
	}

	rows := make([]row, 0, len(tasks))
	for _, t := range tasks {
		unlock := plan.Unlocked(planTask(t), byURL, prs)
		state, reason := plan.Status(plan.Facts{Task: planTask(t), Unlock: unlock, Now: now})
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
