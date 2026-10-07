package web

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

//go:embed page.tmpl
var pageSource string

//go:embed assets all:assets/dist
var assetsDir embed.FS

var page = template.Must(template.New("page").
	Funcs(template.FuncMap{
		"head": func(r *view.Row, scope, featureScope string) rowSlot {
			return newRowSlot(*r, true, 0, scope, featureScope)
		},
		"child": func(r view.Row, depth int, scope, featureScope string) rowSlot {
			return newRowSlot(r, false, depth, scope, featureScope)
		},
		"confirmation": confirmation,
		"percent":      view.PercentOf,
		"raw":          func(s string) template.HTML { return template.HTML(s) },
	}).
	Parse(pageSource))

//go:embed band.tmpl
var bandSource string

var _ = template.Must(page.New("band").Parse(bandSource))

//go:embed board.tmpl
var boardSource string

var boardFragment = template.Must(page.New("board").Parse(boardSource))

//go:embed masthead.tmpl
var mastheadSource string

var _ = template.Must(page.New("masthead").Parse(mastheadSource))

//go:embed layout.tmpl
var layoutSource string

var _ = template.Must(page.New("layout").Parse(layoutSource))

//go:embed boardswap.tmpl
var boardSwapSource string

var boardSwap = template.Must(page.New("boardSwap").Parse(boardSwapSource))

//go:embed detail.tmpl
var detailSource string

var _ = template.Must(boardFragment.New("detail").Parse(detailSource))

// html/template resets $ to the invoked subtemplate's own argument, so "row" cannot see
// pageView's copies of these fields.
type rowSlot struct {
	view.Row
	Head         bool
	Depth        int
	LaunchVerb   string
	CancelVerb   string
	FollowUpVerb string
	Scope        string
	FeatureScope string
}

func newRowSlot(r view.Row, head bool, depth int, scope, featureScope string) rowSlot {
	return rowSlot{
		Row: r, Head: head, Depth: depth,
		LaunchVerb: plan.VerbLaunch, CancelVerb: plan.VerbCancel, FollowUpVerb: plan.VerbFollowUp,
		Scope: scope, FeatureScope: featureScope,
	}
}

//go:embed features.tmpl
var featuresSource string

var featuresPage = template.Must(page.New("features").
	Funcs(template.FuncMap{"pathEscape": url.PathEscape}).
	Parse(featuresSource))

//go:embed launch_modal.tmpl
var launchModalSource string

var launchModal = template.Must(template.New("launchModal").Parse(launchModalSource))

// Clock is the server's only source of time, so a test can drive it without sleeping.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// Server is the status page plus the launch, authorisation and features routes. It only queues
// intents; it never writes the database directly.
type Server struct {
	store      *store.Store
	clock      Clock
	repos      []config.Repo
	view       *view.Reader
	trackerFor tracker.Resolver
	rawMux     *http.ServeMux
	mux        http.Handler
	nudge      func()
}

// NewServer assembles the page and its routes over a store, a clock, the configured repos and
// the data directory.
func NewServer(store *store.Store, clock Clock, repos []config.Repo, dataDir string) *Server {
	s := &Server{
		store: store, clock: clock, repos: repos,
		view:       view.NewReader(store, repos, dataDir, renderLogLine),
		trackerFor: tracker.New,
		nudge:      func() {},
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
	mux.HandleFunc("POST /features/{feature}/import", s.handleImportFeature)
	mux.HandleFunc("GET /launch/candidates", s.handleCandidates)
	mux.HandleFunc("GET /events", s.handleEvents)
	mux.HandleFunc("POST /launch/open", s.handleLaunchOpen)
	mux.HandleFunc("POST /launch", s.handleLaunch)
	mux.HandleFunc("POST /verb", s.handleVerb)
	mux.HandleFunc("POST /ticket", s.handleTicket)
	s.rawMux = mux
	s.mux = http.NewCrossOriginProtection().Handler(mux)
	return s
}

// SetTrackerSource replaces the tracker constructor so a test can drive GET /features without gh.
func (s *Server) SetTrackerSource(resolve tracker.Resolver) { s.trackerFor = resolve }

// SetNudge sets the call that wakes the loop once the server has queued an import intent.
func (s *Server) SetNudge(nudge func()) { s.nudge = nudge }

func (s *Server) SetBoardPollSeconds(seconds int) { s.view.SetBoardPollSeconds(seconds) }

func (s *Server) SetSpendLimit5h(pct int) { s.view.SetSpendLimit5h(pct) }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) handleStylesheet(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, assetsDir, "assets/dist/app.css")
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.renderBoard(w, r, page)
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	s.renderBoard(w, r, boardSwap)
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	board, err := s.view.Board(r.Context(), s.clock.Now(), view.ParseParams(r.URL.Query()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, board.Groups)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func renderHTML(w http.ResponseWriter, tmpl *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) renderBoard(w http.ResponseWriter, r *http.Request, tmpl *template.Template) {
	board, err := s.view.Board(r.Context(), s.clock.Now(), view.ParseParams(r.URL.Query()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderHTML(w, tmpl, board)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.Events(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, events)
}

func (s *Server) renderLaunchModal(w http.ResponseWriter, modal view.LaunchModal) {
	renderHTML(w, launchModal, modal)
}

func (s *Server) handleLaunchOpen(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()

	if feature := r.FormValue("feature"); feature != "" {
		if err := s.store.QueueVerbIntent(ctx, feature, store.ImportVerb, s.clock.Now()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.nudge()

		modal, err := s.view.FeatureModal(ctx, s.clock.Now(), feature)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.renderLaunchModal(w, modal)
		return
	}

	requested := r.Form["ticket"]
	if len(requested) == 0 {
		http.Error(w, "either feature or at least one ticket is required", http.StatusBadRequest)
		return
	}
	modal, err := s.view.TicketModal(ctx, s.clock.Now(), requested)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.renderLaunchModal(w, modal)
}

func (s *Server) handleCandidates(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := s.clock.Now()
	q := r.URL.Query()
	if r.Header.Get("HX-Request") != "" {
		if feature := q.Get("feature"); feature != "" {
			modal, err := s.view.FeatureModal(ctx, now, feature)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			s.renderLaunchModal(w, modal)
			return
		}
		requested := q["ticket"]
		if len(requested) == 0 {
			http.Error(w, "either ?feature= or at least one ?ticket= is required", http.StatusBadRequest)
			return
		}
		modal, err := s.view.TicketModal(ctx, now, requested)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.renderLaunchModal(w, modal)
		return
	}

	candidates, err := s.view.Candidates(ctx, now, q)
	if err != nil {
		status := http.StatusInternalServerError
		if view.IsInvalid(err) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, candidates)
}

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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

	snap, err := s.view.Snapshot(ctx, s.clock.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows, err := snap.Preview(requested)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hashes := make(map[string]string, len(rows))
	for _, row := range rows {
		ticketURL := row.Ticket.URL
		if want, ok := previewed[ticketURL]; ok && want != row.PromptHash {
			http.Error(w, fmt.Sprintf("ticket %s was previewed at hash %s and now composes to %s",
				ticketURL, want, row.PromptHash), http.StatusConflict)
			return
		}
		hashes[ticketURL] = row.PromptHash
	}

	group, err := randomGroup()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	now := s.clock.Now()
	for _, ticketURL := range requested {
		if err := s.store.QueueLaunchIntent(ctx, ticketURL, hashes[ticketURL], group, now); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleVerb(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	verb := r.FormValue("verb")
	ticketURL := r.FormValue("ticket")
	if verb == "" || ticketURL == "" {
		http.Error(w, "verb and ticket are both required", http.StatusBadRequest)
		return
	}
	if !plan.IsRowVerb(verb) {
		http.Error(w, fmt.Sprintf("unsupported verb %q", verb), http.StatusBadRequest)
		return
	}

	var prompt string
	if verb == plan.VerbFollowUp {
		prompt = strings.TrimSpace(r.FormValue("prompt"))
		if prompt == "" {
			http.Error(w, "prompt is required for follow-up", http.StatusBadRequest)
			return
		}
	}

	snap, err := s.view.Snapshot(ctx, s.clock.Now())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	entry, ok := snap.Entry(ticketURL)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown ticket %q", ticketURL), http.StatusBadRequest)
		return
	}
	if !snap.Offers(ticketURL, verb) {
		http.Error(w, fmt.Sprintf("%s is %s and does not offer %s: %s", ticketURL, entry.State, verb, entry.Reason),
			http.StatusConflict)
		return
	}

	if verb == plan.VerbFollowUp {
		err = s.store.QueueVerbIntentWithPayload(ctx, ticketURL, verb, prompt, s.clock.Now())
	} else {
		err = s.store.QueueVerbIntent(ctx, ticketURL, verb, s.clock.Now())
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderBoard(w, r, boardSwap)
}

func nonBlank(values []string) []string {
	var kept []string
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			kept = append(kept, v)
		}
	}
	return kept
}

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
	blockedBy := nonBlank(r.Form["blocked_by"])

	tickets, err := s.store.Tickets(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ticket, ok := ticketByURL(tickets, ticketURL)
	if !ok {
		http.Error(w, fmt.Sprintf("unknown ticket %q", ticketURL), http.StatusBadRequest)
		return
	}

	if branch != ticket.Branch {
		obs, _, err := s.store.LastObservation(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if worktreePath, ok := obs.Worktrees[plan.BranchKey(ticket.Repo, ticket.Branch)]; ok {
			http.Error(w, fmt.Sprintf("branch %s already has a worktree at %s, remove it before changing branch",
				ticket.Branch, worktreePath), http.StatusConflict)
			return
		}
	}

	if err := s.store.QueueEditTicketIntent(ctx, ticketURL, branch, blockedBy, s.clock.Now()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	all, err := importFeatures(ctx, s.repos, s.trackerFor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	features, err := s.view.Features(ctx, s.clock.Now(), all, r.URL.Query().Get("q"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderHTML(w, featuresPage, features)
}

func ticketByURL(tickets []store.Ticket, url string) (store.Ticket, bool) {
	for _, t := range tickets {
		if t.URL == url {
			return t, true
		}
	}
	return store.Ticket{}, false
}

func (s *Server) handleFeatureRedirect(w http.ResponseWriter, r *http.Request) {
	feature := r.PathValue("feature")
	http.Redirect(w, r, "/?feature="+url.QueryEscape(feature), http.StatusSeeOther)
}

func (s *Server) handleImportFeature(w http.ResponseWriter, r *http.Request) {
	feature := r.PathValue("feature")
	ctx := r.Context()
	if err := s.store.QueueVerbIntent(ctx, feature, store.ImportVerb, s.clock.Now()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.nudge()

	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderBoard(w, r, boardSwap)
}

func randomGroup() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate launch group: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
