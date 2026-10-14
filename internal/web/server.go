package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

//go:embed assets all:assets/dist
var assetsDir embed.FS

//go:embed *.tmpl
var templateFiles embed.FS

var templates = template.Must(template.New("").
	Funcs(template.FuncMap{
		"slot":         newRowSlot,
		"confirmation": confirmation,
		"percent":      view.PercentOf,
		"raw":          func(s string) template.HTML { return template.HTML(s) },
		"pathEscape":   url.PathEscape,
		"queryEscape":  url.QueryEscape,
	}).
	ParseFS(templateFiles, "*.tmpl"))

// html/template resets $ to the invoked subtemplate's own argument, so "row" cannot see
// pageView's copies of these fields.
type rowSlot struct {
	view.Row
	LaunchVerb   plan.Verb
	CancelVerb   plan.Verb
	FollowUpVerb plan.Verb
	Scope        string
	FeatureScope string
}

func newRowSlot(r view.Row, scope, featureScope string) rowSlot {
	return rowSlot{
		Row:        r,
		LaunchVerb: plan.VerbLaunch, CancelVerb: plan.VerbCancel, FollowUpVerb: plan.VerbFollowUp,
		Scope: scope, FeatureScope: featureScope,
	}
}

// Server is the status page plus the launch, authorisation and features routes. It only queues
// intents; it never writes the database directly.
type Server struct {
	logger           *slog.Logger
	store            *store.Store
	clock            Clock
	view             *view.Reader
	trackerFor       tracker.Resolver
	mux              http.Handler
	nudger           Nudger
	pushable         pushableCache
	loc              *time.Location
	spendLimit5h     int
	boardPollSeconds int
}

const pushableTTL = time.Minute

// pushableCache holds the last successful gh listing for pushableTTL, so a burst of keystrokes
// makes one gh call.
type pushableCache struct {
	mu      sync.Mutex
	list    func(context.Context) ([]gh.RepoSummary, error)
	repos   []gh.RepoSummary
	fetched time.Time
}

func (c *pushableCache) get(ctx context.Context, now time.Time) ([]gh.RepoSummary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetched.IsZero() && now.Sub(c.fetched) < pushableTTL {
		return c.repos, nil
	}
	repos, err := c.list(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pushable repos: %w", err)
	}
	c.repos, c.fetched = repos, now
	return repos, nil
}

func NewServer(d Deps, opts ...Option) *Server {
	s := &Server{
		logger: d.Logger, store: d.Store, clock: d.Clock, nudger: d.Nudger,
		trackerFor:       tracker.New,
		pushable:         pushableCache{list: gh.PushableRepos},
		boardPollSeconds: config.DefaultBoardPollSeconds,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.view = view.NewReader(d.Store, d.DataDir, renderLogLine, s.boardPollSeconds, s.spendLimit5h)
	mux := http.NewServeMux()
	mux.Handle("GET /{$}", s.handle(s.handleIndex))
	mux.Handle("GET /tickets", s.handle(s.handleTickets))
	mux.Handle("GET /f/{feature}", s.handle(s.handleFeature))
	mux.Handle("GET /f/{feature}/graph", s.handle(s.handleFeatureGraph))
	mux.Handle("GET /f/{feature}/launch", s.handle(s.handleFeatureLaunch))
	mux.Handle("GET /launch", s.handle(s.handleLaunchPicker))
	mux.Handle("GET /board", s.handle(s.handleBoard))
	mux.Handle("GET /rail", s.handle(s.handleRail))
	mux.Handle("GET /graph.json", s.handle(s.handleGraph))
	mux.Handle("GET /s/{owner}/{name}/{n}", s.handle(s.handleSession))
	mux.Handle("GET /insights", s.handle(s.handleInsightsPage))
	mux.Handle("GET /insights.json", s.handle(s.handleInsights))
	mux.Handle("GET /assets/", http.FileServerFS(assetsDir))
	mux.HandleFunc("GET /assets/app.css", s.handleStylesheet)
	mux.Handle("GET /ticket/{ticket}/log", s.handle(s.handleLog))
	mux.Handle("GET /repos", s.handle(s.handleRepos))
	mux.Handle("GET /repos/{owner}/{name}", s.handle(s.handleRepo))
	mux.Handle("GET /repos/search", s.handle(s.handleRepoSearch))
	mux.Handle("GET /repos/banner", s.handle(s.handleBanner))
	mux.Handle("POST /repos/track", s.handle(s.handleTrack))
	mux.Handle("POST /projects", s.handle(s.handleCreateProject))
	mux.Handle("POST /projects/{name}", s.handle(s.handleEditProject))
	mux.Handle("POST /projects/{name}/delete", s.handle(s.handleDeleteProject))
	mux.Handle("POST /repos/{owner}/{name}/project", s.handle(s.handleSetRepoProject))
	mux.Handle("POST /features/{feature}/import", s.handle(s.handleImportFeature))
	mux.Handle("GET /launch/candidates", s.handle(s.handleCandidates))
	mux.Handle("GET /events", s.handle(s.handleEvents))
	mux.Handle("POST /launch/open", s.handle(s.handleLaunchOpen))
	mux.Handle("POST /launch", s.handle(s.handleLaunch))
	mux.Handle("POST /verb", s.handle(s.handleVerb))
	mux.Handle("POST /ticket", s.handle(s.handleTicket))
	s.mux = http.NewCrossOriginProtection().Handler(mux)
	return s
}

func (s *Server) nudge() {
	if s.nudger != nil {
		s.nudger.Nudge()
	}
}

func (s *Server) now() time.Time {
	if s.loc != nil {
		return s.clock.Now().In(s.loc)
	}
	return s.clock.Now()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) handle(h func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			s.writeError(w, r, err)
		}
	})
}

func clientMessage(err error) string {
	if se, ok := errors.AsType[statusError](err); ok {
		return se.Error()
	}
	if invalid, ok := errors.AsType[view.InvalidError](err); ok {
		return invalid.Error()
	}
	for _, m := range statusMap {
		if !errors.Is(err, m.target) {
			continue
		}
		e := err
		for {
			next := errors.Unwrap(e)
			if next == nil || errors.Unwrap(next) == nil || !errors.Is(next, m.target) {
				break
			}
			e = next
		}
		return e.Error()
	}
	return err.Error()
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := statusOf(err)
	if status >= http.StatusInternalServerError {
		s.logger.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		http.Error(w, "internal error", status)
		return
	}
	if r.Header.Get("HX-Request") == "" {
		http.Error(w, clientMessage(err), status)
		return
	}
	w.Header().Set("HX-Retarget", "#flash")
	w.Header().Set("HX-Reswap", "innerHTML")
	w.Header().Set("HX-Reselect", "p.banner")
	if renderErr := renderHTMLStatus(w, http.StatusUnprocessableEntity, "flash", clientMessage(err)); renderErr != nil {
		s.logger.ErrorContext(r.Context(), "render flash", "err", renderErr)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

type statusError struct {
	status int
	err    error
}

func (e statusError) Error() string { return e.err.Error() }
func (e statusError) Unwrap() error { return e.err }

func withStatus(status int, err error) error { return statusError{status: status, err: err} }

func errorf(status int, format string, args ...any) error {
	return withStatus(status, fmt.Errorf(format, args...))
}

var statusMap = []struct {
	target error
	status int
}{
	{store.ErrProjectExists, http.StatusConflict},
	{store.ErrNotFound, http.StatusNotFound},
	{store.ErrInvalidProject, http.StatusBadRequest},
	{view.ErrSessionNotFound, http.StatusNotFound},
}

func statusOf(err error) int {
	if se, ok := errors.AsType[statusError](err); ok {
		return se.status
	}
	if view.IsInvalid(err) {
		return http.StatusBadRequest
	}
	for _, m := range statusMap {
		if errors.Is(err, m.target) {
			return m.status
		}
	}
	return http.StatusInternalServerError
}

func (s *Server) handleStylesheet(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, assetsDir, "assets/dist/app.css")
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) error {
	home, err := s.view.Home(r.Context(), s.clock.Now())
	if err != nil {
		return fmt.Errorf("load home: %w", err)
	}
	return renderHTML(w, "home.tmpl", home)
}

func (s *Server) handleTickets(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	q.Set("all", "1")
	board, err := s.view.Board(r.Context(), s.clock.Now(), view.ParseParams(q))
	if err != nil {
		return fmt.Errorf("load board: %w", err)
	}
	return renderHTML(w, "tickets.tmpl", board)
}

func (s *Server) handleFeature(w http.ResponseWriter, r *http.Request) error {
	return s.renderFeature(w, r, "feature.tmpl")
}

func (s *Server) handleFeatureGraph(w http.ResponseWriter, r *http.Request) error {
	return s.renderFeature(w, r, "feature_graph.tmpl")
}

func (s *Server) renderFeature(w http.ResponseWriter, r *http.Request, tmpl string) error {
	board, err := s.featureBoard(r)
	if err != nil {
		return fmt.Errorf("load feature board: %w", err)
	}
	return renderHTML(w, tmpl, board)
}

func (s *Server) featureBoard(r *http.Request) (view.Board, error) {
	feature := r.PathValue("feature")
	q := r.URL.Query()
	q.Set("feature", feature)
	board, err := s.view.Board(r.Context(), s.clock.Now(), view.ParseParams(q))
	if err != nil {
		return view.Board{}, fmt.Errorf("load board: %w", err)
	}
	if board.FeatureScope != feature {
		return view.Board{}, errorf(http.StatusNotFound, "no feature %q", feature)
	}
	return board, nil
}

func (s *Server) handleFeatureLaunch(w http.ResponseWriter, r *http.Request) error {
	board, err := s.featureBoard(r)
	if err != nil {
		return fmt.Errorf("load feature board: %w", err)
	}
	modal, err := s.view.FeatureModal(r.Context(), s.clock.Now(), board.FeatureScope)
	if err != nil {
		return fmt.Errorf("load feature modal: %w", err)
	}
	board.Launch = option.Some(modal)
	return renderHTML(w, "feature.tmpl", board)
}

func (s *Server) handleLaunchPicker(w http.ResponseWriter, r *http.Request) error {
	page, err := s.view.LaunchPicker(r.Context(), s.clock.Now())
	if err != nil {
		return fmt.Errorf("load launch picker: %w", err)
	}
	return renderHTML(w, "launch_picker.tmpl", page)
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) error {
	return s.renderBoard(w, r, "board")
}

const railOpenCookie = "rail-open"

func (s *Server) handleRail(w http.ResponseWriter, r *http.Request) error {
	currentURL := r.Header.Get("HX-Current-URL")
	params := view.RailParams{
		Sel:   view.RailSelection(currentURL),
		Query: strings.TrimSpace(r.FormValue("q")),
	}
	params.Project, params.Repo = view.RailScope(currentURL)
	if c, err := r.Cookie(railOpenCookie); err == nil {
		if value, err := url.QueryUnescape(c.Value); err == nil && value != "" {
			params.Open = strings.Split(value, view.RailOpenSeparator)
		}
	}
	rail, err := s.view.Rail(r.Context(), s.clock.Now(), params)
	if err != nil {
		return fmt.Errorf("load rail: %w", err)
	}
	return renderHTML(w, "rail", rail)
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) error {
	board, err := s.view.Board(r.Context(), s.clock.Now(), view.ParseParams(r.URL.Query()))
	if err != nil {
		return fmt.Errorf("load board: %w", err)
	}
	return writeJSON(w, board.Groups)
}

func writeJSON(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}

func renderHTML(w http.ResponseWriter, name string, data any) error {
	return renderHTMLStatus(w, http.StatusOK, name, data)
}

func renderHTMLStatus(w http.ResponseWriter, status int, name string, data any) error {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}

func (s *Server) renderBoard(w http.ResponseWriter, r *http.Request, name string) error {
	board, err := s.view.Board(r.Context(), s.clock.Now(), view.ParseParams(r.URL.Query()))
	if err != nil {
		return fmt.Errorf("load board: %w", err)
	}
	return renderHTML(w, name, board)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) error {
	events, err := s.store.Events(r.Context())
	if err != nil {
		return fmt.Errorf("load events: %w", err)
	}
	return writeJSON(w, events)
}

func renderLaunchModal(w http.ResponseWriter, modal view.LaunchModal) error {
	return renderHTML(w, "launchDialog", modal)
}

func (s *Server) handleLaunchOpen(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return withStatus(http.StatusBadRequest, err)
	}
	ctx := r.Context()

	if feature := r.FormValue("feature"); feature != "" {
		if err := s.store.QueueVerbIntent(ctx, feature, plan.VerbImport, s.clock.Now()); err != nil {
			return fmt.Errorf("queue verb intent: %w", err)
		}
		s.nudge()

		modal, err := s.view.FeatureModal(ctx, s.clock.Now(), feature)
		if err != nil {
			return fmt.Errorf("load feature modal: %w", err)
		}
		return renderLaunchModal(w, modal)
	}

	requested := r.Form["ticket"]
	if len(requested) == 0 {
		return errorf(http.StatusBadRequest, "either feature or at least one ticket is required")
	}
	modal, err := s.view.TicketModal(ctx, s.clock.Now(), requested)
	if err != nil {
		return withStatus(http.StatusBadRequest, err)
	}
	return renderLaunchModal(w, modal)
}

func (s *Server) handleCandidates(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	now := s.clock.Now()
	q := r.URL.Query()
	if r.Header.Get("HX-Request") != "" {
		if feature := q.Get("feature"); feature != "" {
			modal, err := s.view.FeatureModal(ctx, now, feature)
			if err != nil {
				return fmt.Errorf("load feature modal: %w", err)
			}
			return renderLaunchModal(w, modal)
		}
		requested := q["ticket"]
		if len(requested) == 0 {
			return errorf(http.StatusBadRequest, "either ?feature= or at least one ?ticket= is required")
		}
		modal, err := s.view.TicketModal(ctx, now, requested)
		if err != nil {
			return withStatus(http.StatusBadRequest, err)
		}
		return renderLaunchModal(w, modal)
	}

	candidates, err := s.view.Candidates(ctx, now, q)
	if err != nil {
		return fmt.Errorf("load candidates: %w", err)
	}
	return writeJSON(w, candidates)
}

func (s *Server) handleLaunch(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		return withStatus(http.StatusBadRequest, err)
	}
	requested := r.Form["ticket"]
	if len(requested) == 0 {
		return errorf(http.StatusBadRequest, "at least one ticket is required")
	}
	// One `hash` field per launchable row, each naming its own ticket: an unchecked row still posts
	// its hidden hash, so pairing by position would pair the survivors wrong.
	previewed := make(map[string]string, len(r.Form["hash"]))
	for _, field := range r.Form["hash"] {
		ticketURL, hash, ok := strings.Cut(field, " ")
		if !ok {
			return errorf(http.StatusBadRequest, "malformed hash field %q, want \"<ticket> <hash>\"", field)
		}
		previewed[ticketURL] = hash
	}

	snap, err := s.view.Snapshot(ctx, s.clock.Now())
	if err != nil {
		return fmt.Errorf("load snapshot: %w", err)
	}
	rows, err := snap.Preview(plan.TicketURLs(requested))
	if err != nil {
		return withStatus(http.StatusBadRequest, err)
	}
	hashes := make(map[string]string, len(rows))
	for _, row := range rows {
		ticketURL := string(row.Ticket.URL)
		if want, ok := previewed[ticketURL]; ok && want != row.PromptHash {
			return errorf(http.StatusConflict, "ticket %s was previewed at hash %s and now composes to %s",
				ticketURL, want, row.PromptHash)
		}
		hashes[ticketURL] = row.PromptHash
	}

	group, err := randomGroup()
	if err != nil {
		return fmt.Errorf("pick launch group: %w", err)
	}

	now := s.clock.Now()
	for _, ticketURL := range requested {
		if err := s.store.QueueLaunchIntent(ctx, ticketURL, hashes[ticketURL], group, now); err != nil {
			return fmt.Errorf("queue launch intent: %w", err)
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
	return nil
}

func (s *Server) handleVerb(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	rawVerb := r.FormValue("verb")
	ticketURL := r.FormValue("ticket")
	if rawVerb == "" || ticketURL == "" {
		return errorf(http.StatusBadRequest, "verb and ticket are both required")
	}
	verb, err := plan.ParseVerb(rawVerb)
	if err != nil || !verb.IsRowVerb() {
		return errorf(http.StatusBadRequest, "unsupported verb %q", rawVerb)
	}

	var prompt string
	if verb == plan.VerbFollowUp {
		prompt = strings.TrimSpace(r.FormValue("prompt"))
		if prompt == "" {
			return errorf(http.StatusBadRequest, "prompt is required for follow-up")
		}
	}

	snap, err := s.view.Snapshot(ctx, s.clock.Now())
	if err != nil {
		return fmt.Errorf("load snapshot: %w", err)
	}
	entry, ok := snap.Entry(plan.TicketURL(ticketURL))
	if !ok {
		return errorf(http.StatusBadRequest, "unknown ticket %q", ticketURL)
	}
	if !snap.Offers(plan.TicketURL(ticketURL), verb) {
		return errorf(http.StatusConflict, "%s is %s and does not offer %s: %s",
			ticketURL, entry.State, verb, entry.Reason)
	}

	if verb == plan.VerbFollowUp {
		err = s.store.QueueVerbIntentWithPayload(ctx, ticketURL, verb, prompt, s.clock.Now())
	} else {
		err = s.store.QueueVerbIntent(ctx, ticketURL, verb, s.clock.Now())
	}
	if err != nil {
		return fmt.Errorf("queue verb intent: %w", err)
	}
	if r.FormValue("from") == "rail" && r.Header.Get("HX-Request") != "" {
		return s.handleRail(w, r)
	}
	if r.FormValue("from") == "session" {
		return s.afterSessionVerb(w, r, ticketURL, verb)
	}
	return s.redirectOrSwap(w, r)
}

func (s *Server) redirectOrSwap(w http.ResponseWriter, r *http.Request) error {
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return nil
	}
	return s.renderBoard(w, r, "board")
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

func (s *Server) handleTicket(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		return withStatus(http.StatusBadRequest, err)
	}
	ticketURL := r.FormValue("ticket")
	branch := r.FormValue("branch")
	if ticketURL == "" || branch == "" {
		return errorf(http.StatusBadRequest, "ticket and branch are both required")
	}
	blockedBy := nonBlank(r.Form["blocked_by"])

	tickets, err := s.store.Tickets(ctx)
	if err != nil {
		return fmt.Errorf("load tickets: %w", err)
	}
	ticket, ok := ticketByURL(tickets, ticketURL)
	if !ok {
		return errorf(http.StatusBadRequest, "unknown ticket %q", ticketURL)
	}

	if branch != ticket.Branch {
		obs, _, err := s.store.LastObservation(ctx)
		if err != nil {
			return fmt.Errorf("load last observation: %w", err)
		}
		if worktreePath, ok := obs.Worktrees[ticket.Ref()]; ok {
			return errorf(http.StatusConflict, "branch %s already has a worktree at %s, remove it before changing branch",
				ticket.Branch, worktreePath)
		}
	}

	if err := s.store.QueueEditTicketIntent(ctx, ticketURL, branch, blockedBy, s.clock.Now()); err != nil {
		return fmt.Errorf("queue edit ticket intent: %w", err)
	}
	http.Redirect(w, r, afterTicketEdit(r.FormValue("return")), http.StatusSeeOther)
	return nil
}

func afterTicketEdit(back string) string {
	u, err := url.Parse(back)
	if err != nil || u.Scheme != "" || u.Host != "" ||
		!strings.HasPrefix(u.Path, "/s/") || strings.Contains(u.Path, "..") {
		return "/"
	}
	return u.EscapedPath()
}

func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) error {
	page, err := s.view.Repos(r.Context(), s.clock.Now())
	if err != nil {
		return fmt.Errorf("load repos: %w", err)
	}
	return renderHTML(w, "repos.tmpl", page)
}

func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	scope := r.PathValue("owner") + "/" + r.PathValue("name")
	if !repoName.MatchString(scope) || hasDotSegment(scope) {
		return errorf(http.StatusNotFound, "repo %q is not owner/name", scope)
	}
	repo, known, err := s.view.KnownRepo(ctx, scope)
	if err != nil {
		return fmt.Errorf("load known repo: %w", err)
	}
	var offered []string
	if known && repo.Phase == plan.RepoReady {
		obs, _, err := s.store.LastObservation(ctx)
		if err != nil {
			return fmt.Errorf("load last observation: %w", err)
		}
		if offered, err = importFeatures(ctx, []store.Repo{repo}, obs.Settings, s.trackerFor); err != nil {
			return fmt.Errorf("import features: %w", err)
		}
	}
	req := view.RepoPageRequest{Scope: scope, Offered: offered, Selected: r.URL.Query()["ticket"]}
	page, err := s.view.RepoPage(ctx, s.clock.Now(), req)
	if err != nil {
		return fmt.Errorf("load repo page: %w", err)
	}
	return renderHTML(w, "repo.tmpl", page)
}

func (s *Server) handleBanner(w http.ResponseWriter, r *http.Request) error {
	banner, err := s.view.Banner(r.Context(), r.URL.Query().Get("repo"))
	if err != nil {
		return fmt.Errorf("load banner: %w", err)
	}
	rawSeen := r.URL.Query().Get("seen")
	if rawSeen != "" {
		if seen, err := view.ParseBannerState(rawSeen); err != nil || seen != banner.State {
			w.Header().Set("HX-Refresh", "true")
		}
	}
	if rawSeen != "" && !banner.Polling() {
		return renderHTMLStatus(w, 286, "repoBanner", banner)
	}
	return renderHTML(w, "repoBanner", banner)
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func hasDotSegment(repo string) bool {
	return slices.ContainsFunc(strings.Split(repo, "/"), func(seg string) bool { return seg == "." || seg == ".." })
}

func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) error {
	repo := strings.TrimSpace(r.FormValue("repo"))
	if !repoName.MatchString(repo) || hasDotSegment(repo) {
		return errorf(http.StatusBadRequest, "repo %q is not owner/name", repo)
	}
	queued, err := s.store.QueueTrackIntent(r.Context(), repo, s.clock.Now())
	if err != nil {
		return fmt.Errorf("queue track intent: %w", err)
	}
	if queued {
		s.nudge()
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, view.RepoPath(repo), http.StatusSeeOther)
		return nil
	}
	banner, err := s.view.Banner(r.Context(), repo)
	if err != nil {
		return fmt.Errorf("load banner: %w", err)
	}
	return renderHTML(w, "repoBanner", banner)
}

func (s *Server) handleRepoSearch(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query().Get("q")
	if strings.TrimSpace(query) == "" {
		return s.renderRepoGroups(w, r)
	}
	var (
		pushable []gh.RepoSummary
		search   view.RepoSearch
	)
	if strings.TrimSpace(query) != "" {
		var err error
		if pushable, err = s.pushable.get(r.Context(), s.clock.Now()); err != nil {
			search.SearchError = err.Error()
		}
	}
	rows, err := s.view.SearchRepos(r.Context(), query, pushable)
	if err != nil {
		return fmt.Errorf("search repos: %w", err)
	}
	search.Rows = rows
	return renderHTML(w, "repoResults", search)
}

func ticketByURL(tickets []store.Ticket, url string) (store.Ticket, bool) {
	for _, t := range tickets {
		if t.URL == url {
			return t, true
		}
	}
	return store.Ticket{}, false
}

func (s *Server) handleImportFeature(w http.ResponseWriter, r *http.Request) error {
	feature := r.PathValue("feature")
	if err := s.store.QueueVerbIntent(r.Context(), feature, plan.VerbImport, s.clock.Now()); err != nil {
		return fmt.Errorf("queue verb intent: %w", err)
	}
	s.nudge()
	return s.redirectOrSwap(w, r)
}

func randomGroup() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate launch group: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
