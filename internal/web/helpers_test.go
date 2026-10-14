package web_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/option"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	return cctest.Store(t)
}

type frozenClock struct{ at time.Time }

func (c frozenClock) Now() time.Time { return c.at }

func fixedClock(at time.Time) web.Clock { return frozenClock{at} }

func dispositionAsPushed(t *testing.T, st *store.Store, ticketURL string, at time.Time) {
	t.Helper()
	runID, err := st.InsertRunSkeleton(t.Context(), store.RunSkeleton{
		Ticket: ticketURL, Kind: "agent", PromptHash: "hash-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSpawn(t.Context(), store.Spawn{
		RunID: plan.RunID(runID), Pgid: 111, StartedAt: at, LogPath: "/state/runs/1.jsonl",
	}); err != nil {
		t.Fatal(err)
	}
	exitCode := 0
	if err := st.RecordDisposition(t.Context(), store.Disposition{
		RunID: plan.RunID(runID), Outcome: plan.OutcomePush, ExitCode: option.Some(exitCode),
		Cost: option.None[float64](), EndedAt: at,
	}); err != nil {
		t.Fatal(err)
	}
}

var testNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func openServer(st *store.Store, clock web.Clock, dataDir string, opts ...web.Option) *web.Server {
	deps := web.Deps{Logger: slog.New(slog.DiscardHandler), Store: st, Clock: clock, DataDir: dataDir}
	return web.NewServer(deps, opts...)
}

type nudgeFunc func()

func (f nudgeFunc) Nudge() { f() }

func openNudgingServer(st *store.Store, nudge func(), opts ...web.Option) *web.Server {
	return web.NewServer(web.Deps{
		Logger: slog.New(slog.DiscardHandler), Store: st, Clock: fixedClock(testNow), Nudger: nudgeFunc(nudge),
	}, opts...)
}

func newServer(st *store.Store, now time.Time, opts ...web.Option) *web.Server {
	return openServer(st, fixedClock(now), "", opts...)
}

type req struct {
	Form   url.Values
	Header http.Header
	Origin string
}

type resp struct {
	Code     int
	Header   http.Header
	Body     string
	Location string
}

func do(t *testing.T, h http.Handler, method, target string, r req) resp {
	t.Helper()
	body := ""
	if r.Form != nil {
		body = r.Form.Encode()
	}
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if r.Form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for key, values := range r.Header {
		request.Header[key] = slices.Clone(values)
	}
	if r.Origin != "-" {
		origin := r.Origin
		if origin == "" {
			origin = "http://" + request.Host
		}
		request.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request)
	return resp{
		Code: rec.Code, Header: rec.Header().Clone(), Body: rec.Body.String(),
		Location: rec.Header().Get("Location"),
	}
}

func isFragmentRoute(path string) bool {
	if strings.HasPrefix(path, "/s/") {
		return strings.Contains(path, "/actions")
	}
	return slices.ContainsFunc(
		[]string{"/rail", "/board", "/repos/search", "/repos/banner", "/launch/candidates?"},
		func(route string) bool { return strings.HasPrefix(path, route) },
	)
}

func track(t *testing.T, st *store.Store, repos ...store.Repo) *store.Store {
	t.Helper()
	for _, repo := range repos {
		repo.Phase, repo.TrackedAt = plan.RepoReady, testNow
		if err := st.UpsertRepo(t.Context(), repo); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func named(names ...string) []store.Repo {
	repos := make([]store.Repo, 0, len(names))
	for _, name := range names {
		repos = append(repos, store.Repo{Name: name})
	}
	return repos
}
