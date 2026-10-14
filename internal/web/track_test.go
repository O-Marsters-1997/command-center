package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func TestTrackQueuesOneIntentWithoutConsultingTracker(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	source := web.WithTrackerSource(func(tracker.Kind, string) (tracker.Source, error) {
		t.Error("track handler consulted tracker source")
		return nil, nil
	})
	server := openServer(st, fixedClock(testNow), "", source)

	request := req{Form: url.Values{"repo": {"acme/new"}}, Header: http.Header{"Hx-Request": {"true"}}}
	first := do(t, server, http.MethodPost, "/repos/track", request)
	second := do(t, server, http.MethodPost, "/repos/track", request)

	for _, rec := range []resp{first, second} {
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body, `data-state="cloning"`) {
			t.Errorf("POST /repos/track = %d, want 200 reading cloning:\n%s", rec.Code, rec.Body)
		}
	}
	intents, err := st.PendingVerbIntents(t.Context(), plan.VerbTrack)
	if err != nil || len(intents) != 1 || intents[0].TicketID != "acme/new" {
		t.Errorf("pending track intents = %+v (err %v), want exactly acme/new", intents, err)
	}
	repos, err := st.Repos(t.Context())
	if err != nil || len(repos) != 0 {
		t.Errorf("repos = %+v (err %v), want the handler to write no row", repos, err)
	}
}

func TestTrackWithoutHTMXRedirectsToTheRepoPage(t *testing.T) {
	t.Parallel()
	server := openServer(openStore(t), fixedClock(testNow), "")

	rec := do(t, server, http.MethodPost, "/repos/track", req{Form: url.Values{"repo": {"acme/new"}}})

	if rec.Code != http.StatusSeeOther || rec.Location != "/repos/acme/new" {
		t.Errorf("status = %d, Location = %q", rec.Code, rec.Location)
	}
}

func TestTrackRejectsANameThatIsNotOwnerSlashName(t *testing.T) {
	st := openStore(t)
	server := openServer(st, fixedClock(testNow), "")

	for _, tt := range []struct{ name, repo string }{
		{name: "empty"},
		{name: "missing repo", repo: "acme"},
		{name: "extra segment", repo: "acme/a/b"},
		{name: "space", repo: "acme/ x"},
		{name: "parent segments", repo: "../.."},
		{name: "dot segments", repo: "./."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := req{Form: url.Values{"repo": {tt.repo}}, Header: http.Header{"Hx-Request": {"true"}}}
			rec := do(t, server, http.MethodPost, "/repos/track", request)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("Track %q = %d, want 422", tt.repo, rec.Code)
			}
		})
	}
	intents, err := st.PendingVerbIntents(t.Context(), plan.VerbTrack)
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 0 {
		t.Errorf("pending track intents = %+v, want none", intents)
	}
}

func TestBannerPollsUntilTheStateChangesThenAsksForARefresh(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	server := openServer(st, fixedClock(testNow), "")
	request := req{Form: url.Values{"repo": {"acme/new"}}, Header: http.Header{"Hx-Request": {"true"}}}
	do(t, server, http.MethodPost, "/repos/track", request)

	bannerRequest := req{Header: http.Header{"Hx-Request": {"true"}}}
	stale := do(t, server, http.MethodGet, "/repos/banner?repo=acme/new&seen=cloning", bannerRequest)
	if stale.Header.Get("HX-Refresh") != "" || !strings.Contains(stale.Body, `hx-trigger="every 2s"`) {
		t.Errorf("a cloning banner seen as cloning must poll without a refresh:\n%s", stale.Body)
	}

	repo := storepkg.Repo{
		Name: "acme/new", Remote: "git@github.com:acme/new.git", Phase: plan.RepoReady, TrackedAt: testNow,
	}
	if err := st.UpsertRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	consumeTrack(t, st)

	changed := do(t, server, http.MethodGet, "/repos/banner?repo=acme/new&seen=cloning", bannerRequest)
	if changed.Header.Get("HX-Refresh") != "true" {
		t.Errorf("a banner whose state moved from cloning must send HX-Refresh: true")
	}
}

func consumeTrack(t *testing.T, st *storepkg.Store) {
	t.Helper()
	intents, err := st.PendingVerbIntents(t.Context(), plan.VerbTrack)
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range intents {
		if err := st.ConsumeVerbIntent(t.Context(), intent.ID, testNow); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBannerNamesTheRepoState(t *testing.T) {
	t.Parallel()
	repo := func(name string, phase plan.RepoPhase, kind plan.RefusalKind, reason, source string) storepkg.Repo {
		return storepkg.Repo{
			Name: name, Remote: "git@github.com:" + name + ".git", Phase: phase, RefusalKind: kind, Refusal: reason,
			SettingsSource: source, TrackedAt: testNow,
		}
	}
	st := openStore(t)
	for _, r := range []storepkg.Repo{
		repo("acme/cloning", plan.RepoCloning, "", "", ""),
		repo("acme/file", plan.RepoReady, "", "", "origin/main"),
		repo("acme/defaults", plan.RepoReady, "", "", "defaults"),
		repo("acme/merges", plan.RepoRefused, plan.RefusalMergeSettings, "repo acme/merges allows merge commits", ""),
		repo("acme/master", plan.RepoRefused, plan.RefusalDefaultBranch, "default branch is `master`, not `main`", ""),
		repo("acme/noclone", plan.RepoRefused, plan.RefusalClone, "clone failed", ""),
		repo("acme/badtoml", plan.RepoRefused, plan.RefusalSettingsParse, "toml: line 3", ""),
	} {
		if err := st.UpsertRepo(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	server := openServer(st, fixedClock(testNow), "")
	if err := st.QueueVerbIntent(t.Context(), "acme/queued", plan.VerbTrack, testNow); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct{ name, state string }{
		{name: "acme/unknown", state: "untracked"},
		{name: "acme/queued", state: "cloning"},
		{name: "acme/cloning", state: "cloning"},
		{name: "acme/file", state: "ready"},
		{name: "acme/defaults", state: "ready"},
		{name: "acme/merges", state: "refused"},
		{name: "acme/master", state: "refused"},
		{name: "acme/noclone", state: "refused"},
		{name: "acme/badtoml", state: "refused"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := do(t, server, http.MethodGet, "/repos/banner?repo="+tt.name, req{Header: http.Header{"Hx-Request": {"true"}}})
			if rec.Code != http.StatusOK {
				t.Fatalf("GET banner %s: %d: %s", tt.name, rec.Code, rec.Body)
			}
			if want := `data-state="` + tt.state + `"`; !strings.Contains(rec.Body, want) {
				t.Errorf("banner for %s lacks %s:\n%s", tt.name, want, rec.Body)
			}
		})
	}
}

func TestBannerAnswers286OnceTheStateIsTerminal(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	server := openServer(st, fixedClock(testNow), "")
	request := req{Form: url.Values{"repo": {"acme/new"}}, Header: http.Header{"Hx-Request": {"true"}}}
	do(t, server, http.MethodPost, "/repos/track", request)

	bannerRequest := req{Header: http.Header{"Hx-Request": {"true"}}}
	bannerPath := "/repos/banner?repo=acme/new&seen=cloning"
	if rec := do(t, server, http.MethodGet, bannerPath, bannerRequest); rec.Code != http.StatusOK {
		t.Errorf("a cloning banner = %d, want 200 so it keeps polling", rec.Code)
	}

	repo := storepkg.Repo{
		Name: "acme/new", Remote: "git@github.com:acme/new.git", Phase: plan.RepoReady, TrackedAt: testNow,
	}
	if err := st.UpsertRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	consumeTrack(t, st)

	if rec := do(t, server, http.MethodGet, bannerPath, bannerRequest); rec.Code != 286 {
		t.Errorf("a ready banner = %d, want 286 to stop the poll", rec.Code)
	}
}
