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

	"github.com/O-Marsters-1997/command-center/internal/cc"
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
		"destructive": func(verb string) bool { _, ok := destructiveVerbs[verb]; return ok },
		"percent":     percentOf,
		"raw":         func(s string) template.HTML { return template.HTML(s) },
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

// pathEscape joins page's shared FuncMap: Funcs adds to the tree's one function map regardless of
// which member template it is called on, so head, child, destructive and percent see it too.
var featuresPage = template.Must(page.New("features").
	Funcs(template.FuncMap{"pathEscape": url.PathEscape}).
	Parse(featuresSource))

//go:embed launch_modal.tmpl
var launchModalSource string

var launchModal = template.Must(template.New("launchModal").Parse(launchModalSource))

// Clock is the server's only source of time, so a test or the demo can drive it without sleeping.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// Server is the status page plus the launch, launch-authorisation and features routes. It
// never writes the database directly except to queue an intent: every state it shows is derived
// from tickets and the last observation at render time (§5, inv. 14).
type Server struct {
	store      *store.Store
	clock      Clock
	repos      []config.Repo
	view       *view.Reader
	trackerFor cc.TrackerSource
	rawMux     *http.ServeMux
	mux        http.Handler
	nudge      func()
}

// NewServer assembles the page and its routes over a store, a clock, the configured repos and
// the data directory: stacking, the verdict predicate, the mergify hash and the compat check
// name are all per-repo config, and dataDir is the fleet the header names.
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
	mux.HandleFunc("GET /confirm", s.handleConfirm)
	mux.HandleFunc("POST /launch/open", s.handleLaunchOpen)
	mux.HandleFunc("POST /launch", s.handleLaunch)
	mux.HandleFunc("POST /verb", s.handleVerb)
	mux.HandleFunc("POST /ticket", s.handleTicket)
	s.rawMux = mux
	s.mux = http.NewCrossOriginProtection().Handler(mux)
	return s
}

// SetTrackerSource replaces the server's tracker.New, so a test can drive GET /features with a
// fake source rather than shelling out to gh.
func (s *Server) SetTrackerSource(resolve cc.TrackerSource) { s.trackerFor = resolve }

// SetNudge replaces the server's nudge call. App.New wires this to Loop.Nudge once, after both
// exist, which is how the server queues an import intent and wakes the loop without importing
// the loop package itself.
func (s *Server) SetNudge(nudge func()) { s.nudge = nudge }

// SetBoardPollSeconds replaces the interval the board's htmx poll refreshes at.
func (s *Server) SetBoardPollSeconds(seconds int) { s.view.SetBoardPollSeconds(seconds) }

// SetSpendLimit5h replaces the server's copy of spend_limit_5h, so the masthead can name the same
// limit the loop's own launch gate reads (CC-314).
func (s *Server) SetSpendLimit5h(pct int) { s.view.SetSpendLimit5h(pct) }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) handleStylesheet(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, assetsDir, "assets/dist/app.css")
}

func percentOf(part, total int) int {
	if total <= 0 {
		return 0
	}
	return part * 100 / total
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.renderBoard(w, r, page)
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	s.renderBoard(w, r, boardSwap)
}

// handleGraph serves the board's groups verbatim: the same []view.Group the board template ranges
// over, json-tagged rather than reshaped, so the graph island lays out exactly what the board
// renders (docs/prds/prd-fleet-view.md § One derivation).
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

// handleEvents dumps the append-only audit log as JSON: what reconstructs the whole run, every
// authorisation, launch, disposition, push and refusal (docs/prds/prd-command-centre.md § Phase 4).
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
		if err := cc.QueueImport(ctx, s.store, feature, s.clock.Now()); err != nil {
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
	// The swap shows the row's "<verb> queued" pill: the loop's next tick applies the intent, so
	// there is nothing further to render yet.
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderBoard(w, r, boardSwap)
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
	ticket, ok := ticketByURL(tickets, ticketURL)
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

// handleFeatures lists every feature the configured repos' trackers offer, read fresh from the
// tracker on every request (§5, inv. 14).
func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	all, err := cc.ImportFeatures(ctx, s.repos, s.trackerFor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	offered := make([]string, len(all))
	for i, f := range all {
		offered[i] = f.Feature
	}
	page, err := s.view.Features(ctx, s.clock.Now(), offered, r.URL.Query().Get("q"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderHTML(w, featuresPage, page)
}

func ticketByURL(tickets []store.Ticket, url string) (store.Ticket, bool) {
	for _, t := range tickets {
		if t.URL == url {
			return t, true
		}
	}
	return store.Ticket{}, false
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
	if err := cc.QueueImport(ctx, s.store, feature, s.clock.Now()); err != nil {
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

// randomGroup mints the token that ties every intent from one POST /launch call together —
// how N intents are recognised as one launch without a batch-key column.
func randomGroup() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate launch group: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
