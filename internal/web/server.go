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
	"github.com/O-Marsters-1997/command-center/internal/web/hx"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

//go:embed assets all:assets/dist
var assetsDir embed.FS

//go:embed *.tmpl
var templateFiles embed.FS

var templates = template.Must(template.New("").
	Funcs(template.FuncMap{
		"percent":     view.PercentOf,
		"pathEscape":  url.PathEscape,
		"queryEscape": url.QueryEscape,
	}).
	ParseFS(templateFiles, "*.tmpl"))

type Server struct {
	logger     *slog.Logger
	store      *store.Store
	clock      Clock
	view       *view.Reader
	trackerFor tracker.Resolver
	mux        http.Handler
	nudger     Nudger
	pushable   pushableCache
}

const pushableTTL = time.Minute

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
		return nil, fmt.Errorf("fetch pushable repos: list pushable repos: %w", err)
	}
	c.repos, c.fetched = repos, now
	return repos, nil
}

func NewServer(d Deps, opts ...Option) *Server {
	if d.BoardPollSeconds == 0 {
		d.BoardPollSeconds = config.DefaultBoardPollSeconds
	}
	s := &Server{
		logger: d.Logger, store: d.Store, clock: d.Clock, nudger: d.Nudger,
		trackerFor: tracker.New,
		pushable:   pushableCache{list: gh.PushableRepos},
	}
	for _, opt := range opts {
		opt(s)
	}
	s.view = view.NewReader(d.Store, d.DataDir, renderLogLine, d.BoardPollSeconds, d.SpendLimit5h)
	mux := http.NewServeMux()
	mux.Handle("GET /{$}", s.handle(s.handleIndex))
	mux.Handle("GET /tickets", s.handle(s.handleTickets))
	mux.Handle("GET /f/{feature}", s.handle(s.handleFeature))
	mux.Handle("GET /f/{feature}/graph", s.handle(s.handleFeatureGraph))
	mux.Handle("GET /f/{feature}/launch", s.handle(s.handleFeatureLaunch))
	mux.Handle("GET /launch", s.handle(s.handleLaunchPicker))
	mux.Handle("GET /board", s.handle(s.renderBoard))
	mux.Handle("GET /rail", s.handle(s.handleRail))
	mux.Handle("GET /graph.json", s.handle(s.handleGraph))
	mux.Handle("GET /s/{owner}/{name}/{n}", s.handle(s.handleSession))
	mux.Handle("GET /s/{owner}/{name}/{n}/actions", s.handle(s.handleSessionActions))
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
	mux.Handle("GET /launch/candidates.json", s.handle(s.handleCandidatesJSON))
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
	return s.clock.Now().In(time.Local)
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
	if !hx.IsRequest(r) {
		http.Error(w, clientMessage(err), status)
		return
	}
	w.Header().Set(hx.HeaderRetarget, "#flash")
	w.Header().Set(hx.HeaderReswap, "innerHTML")
	w.Header().Set(hx.HeaderReselect, "p.banner")
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
	if _, ok := errors.AsType[view.InvalidError](err); ok {
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
	if requested := r.URL.Query()["ticket"]; len(requested) > 0 {
		modal, err := s.launchModal(r.Context(), s.clock.Now(), "", requested)
		if err != nil {
			return withStatus(http.StatusBadRequest, err)
		}
		page.Launch = &modal
	}
	return renderHTML(w, "launch_picker.tmpl", page)
}

const railOpenCookie = "rail-open"

func (s *Server) handleRail(w http.ResponseWriter, r *http.Request) error {
	currentURL := r.Header.Get(hx.HeaderCurrentURL)
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
	return renderFragment(w, r, "/", "rail", rail)
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) error {
	board, err := s.view.Board(r.Context(), s.clock.Now(), view.ParseParams(r.URL.Query()))
	if err != nil {
		return fmt.Errorf("load board: %w", err)
	}
	return writeJSON(w, view.Graph(board.Groups))
}

func writeJSON(w http.ResponseWriter, v any) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := buf.WriteTo(w); err != nil {
		return fmt.Errorf("write json: %w", err)
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

func (s *Server) renderBoard(w http.ResponseWriter, r *http.Request) error {
	board, err := s.view.Board(r.Context(), s.clock.Now(), view.ParseParams(r.URL.Query()))
	if err != nil {
		return fmt.Errorf("load board: %w", err)
	}
	host := "/tickets"
	if r.URL.RawQuery != "" {
		host += "?" + r.URL.RawQuery
	}
	return renderFragment(w, r, host, "board", board)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) error {
	events, err := s.store.Events(r.Context())
	if err != nil {
		return fmt.Errorf("load events: %w", err)
	}
	return writeJSON(w, events)
}

func (s *Server) handleLaunchOpen(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return withStatus(http.StatusBadRequest, err)
	}
	feature := r.FormValue("feature")
	if feature != "" {
		if err := s.store.QueueVerbIntent(r.Context(), feature, plan.VerbImport, s.clock.Now()); err != nil {
			return fmt.Errorf("queue verb intent: %w", err)
		}
		s.nudge()
	}
	modal, err := s.launchModal(r.Context(), s.clock.Now(), feature, r.Form["ticket"])
	if err != nil {
		return err
	}
	return renderFragment(w, r, modal.Path(), "launchDialog", modal)
}

func (s *Server) handleCandidates(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	modal, err := s.launchModal(r.Context(), s.clock.Now(), q.Get("feature"), q["ticket"])
	if err != nil {
		return err
	}
	return renderFragment(w, r, modal.Path(), "launchDialog", modal)
}

func (s *Server) launchModal(
	ctx context.Context, now time.Time, feature string, tickets []string,
) (view.LaunchModal, error) {
	if feature != "" {
		modal, err := s.view.FeatureModal(ctx, now, feature)
		if err != nil {
			return view.LaunchModal{}, fmt.Errorf("load feature modal: %w", err)
		}
		return modal, nil
	}
	if len(tickets) == 0 {
		return view.LaunchModal{}, errorf(http.StatusBadRequest, "either feature or at least one ticket is required")
	}
	modal, err := s.view.TicketModal(ctx, now, tickets)
	if err != nil {
		return view.LaunchModal{}, withStatus(http.StatusBadRequest, err)
	}
	return modal, nil
}

func (s *Server) handleCandidatesJSON(w http.ResponseWriter, r *http.Request) error {
	candidates, err := s.view.Candidates(r.Context(), s.clock.Now(), r.URL.Query())
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
		launch := store.LaunchRequest{Ticket: ticketURL, PromptHash: hashes[ticketURL], Group: group, At: now}
		if err := s.store.QueueLaunchIntent(ctx, launch); err != nil {
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
		err = s.store.QueueVerbIntentWithPayload(ctx, store.VerbRequest{
			Ticket: ticketURL, Verb: verb, Payload: prompt, At: s.clock.Now(),
		})
	} else {
		err = s.store.QueueVerbIntent(ctx, ticketURL, verb, s.clock.Now())
	}
	if err != nil {
		return fmt.Errorf("queue verb intent: %w", err)
	}
	if r.FormValue("from") == "rail" && hx.IsRequest(r) {
		return s.handleRail(w, r)
	}
	if r.FormValue("from") == "session" {
		return s.afterSessionVerb(w, r, ticketURL, verb)
	}
	return s.redirectOrSwap(w, r)
}

func (s *Server) redirectOrSwap(w http.ResponseWriter, r *http.Request) error {
	if !hx.IsRequest(r) {
		seeOther(w, r, returnPath(r.FormValue("return"), "/", "/"))
		return nil
	}
	return s.renderBoard(w, r)
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

	edit := store.EditRequest{Ticket: ticketURL, Branch: branch, BlockedBy: blockedBy, At: s.clock.Now()}
	if err := s.store.QueueEditTicketIntent(ctx, edit); err != nil {
		return fmt.Errorf("queue edit ticket intent: %w", err)
	}
	http.Redirect(w, r, returnPath(r.FormValue("return"), "/s/", "/"), http.StatusSeeOther)
	return nil
}

func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) error {
	page, err := s.view.Repos(r.Context(), s.clock.Now())
	if err != nil {
		return fmt.Errorf("load repos: %w", err)
	}
	page.Query = r.URL.Query().Get("q")
	if strings.TrimSpace(page.Query) != "" {
		search, err := s.searchRepos(r.Context(), page.Query)
		if err != nil {
			return err
		}
		page.Search = &search
	}
	return renderHTML(w, "repos.tmpl", page)
}

func (s *Server) handleRepo(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	scope := r.PathValue("owner") + "/" + r.PathValue("name")
	if !validRepo(scope) {
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
	repo := r.URL.Query().Get("repo")
	host := "/repos"
	if validRepo(repo) {
		host = view.RepoPath(repo)
	}
	if hx.IsRequest(r) {
		rawSeen := r.URL.Query().Get("seen")
		if rawSeen != "" {
			seen := view.BannerState(rawSeen)
			if !slices.Contains([]view.BannerState{
				view.BannerUntracked, view.BannerCloning, view.BannerReady, view.BannerRefused,
			}, seen) || seen != banner.State {
				w.Header().Set(hx.HeaderRefresh, "true")
			}
		}
		if rawSeen != "" && !banner.Polling() {
			w.Header().Add("Vary", hx.HeaderRequest)
			return renderHTMLStatus(w, 286, "repoBanner", banner)
		}
	}
	return renderFragment(w, r, host, "repoBanner", banner)
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func validRepo(name string) bool {
	return repoName.MatchString(name) && !slices.ContainsFunc(strings.Split(name, "/"), func(seg string) bool {
		return seg == "." || seg == ".."
	})
}

func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) error {
	repo := strings.TrimSpace(r.FormValue("repo"))
	if !validRepo(repo) {
		return errorf(http.StatusBadRequest, "repo %q is not owner/name", repo)
	}
	queued, err := s.store.QueueTrackIntent(r.Context(), repo, s.clock.Now())
	if err != nil {
		return fmt.Errorf("queue track intent: %w", err)
	}
	if queued {
		s.nudge()
	}
	if !hx.IsRequest(r) {
		seeOther(w, r, view.RepoPath(repo))
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
	host := "/repos?q=" + url.QueryEscape(query)
	if strings.TrimSpace(query) == "" {
		groups, err := s.view.RepoGroups(r.Context())
		if err != nil {
			return fmt.Errorf("load repo groups: %w", err)
		}
		return renderFragment(w, r, host, "repoGroups", groups)
	}
	search, err := s.searchRepos(r.Context(), query)
	if err != nil {
		return fmt.Errorf("search repos: %w", err)
	}
	return renderFragment(w, r, host, "repoResults", search)
}

func (s *Server) searchRepos(ctx context.Context, query string) (view.RepoSearch, error) {
	var search view.RepoSearch
	pushable, err := s.pushable.get(ctx, s.clock.Now())
	if err != nil {
		search.SearchError = err.Error()
	}
	rows, err := s.view.SearchRepos(ctx, query, pushable)
	if err != nil {
		return view.RepoSearch{}, fmt.Errorf("search repos: %w", err)
	}
	search.Rows = rows
	return search, nil
}

func ticketByURL(tickets []store.Ticket, ticketURL string) (store.Ticket, bool) {
	for _, t := range tickets {
		if t.URL == ticketURL {
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

func (s *Server) handleInsightsPage(w http.ResponseWriter, r *http.Request) error {
	data, err := s.view.InsightsPage(r.Context(), s.now(), r.URL.Query())
	if err != nil {
		return fmt.Errorf("load insights page: %w", err)
	}
	return renderHTML(w, "insights.tmpl", data)
}

func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) error {
	resp, err := s.view.Insights(r.Context(), s.now(), r.URL.Query())
	if err != nil {
		return fmt.Errorf("load insights: %w", err)
	}
	return writeJSON(w, resp)
}
