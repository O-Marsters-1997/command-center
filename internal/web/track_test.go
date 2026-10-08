package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

func postTrack(t *testing.T, server *web.Server, repo string, hx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/repos/track", strings.NewReader(url.Values{"repo": {repo}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

func recordingGitAndGh(t *testing.T) string {
	t.Helper()
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "calls.log")
	for _, name := range []string{"git", "gh"} {
		script := "#!/bin/sh\necho " + name + " \"$@\" >> " + log + "\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	return log
}

func TestTrackQueuesOneIntentAndRunsNoGitOrGh(t *testing.T) {
	log := recordingGitAndGh(t)
	st := openStore(t)
	server := web.NewServer(st, fixedClock(testNow), "")

	first := postTrack(t, server, "acme/new", true)
	second := postTrack(t, server, "acme/new", true)

	for _, rec := range []*httptest.ResponseRecorder{first, second} {
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-state="cloning"`) {
			t.Errorf("POST /repos/track = %d, want 200 reading cloning:\n%s", rec.Code, rec.Body)
		}
	}
	intents, err := st.PendingVerbIntents(t.Context(), storepkg.TrackVerb)
	if err != nil || len(intents) != 1 || intents[0].TicketID != "acme/new" {
		t.Errorf("pending track intents = %+v (err %v), want exactly acme/new", intents, err)
	}
	repos, err := st.Repos(t.Context())
	if err != nil || len(repos) != 0 {
		t.Errorf("repos = %+v (err %v), want the handler to write no row", repos, err)
	}
	if calls, _ := os.ReadFile(log); len(calls) != 0 {
		t.Errorf("handler ran commands:\n%s", calls)
	}
}

func TestTrackWithoutHTMXRedirectsToTheRepoPage(t *testing.T) {
	server := web.NewServer(openStore(t), fixedClock(testNow), "")

	rec := postTrack(t, server, "acme/new", false)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/features?repo=acme/new" {
		t.Errorf("status = %d, Location = %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestTrackRejectsANameThatIsNotOwnerSlashName(t *testing.T) {
	server := web.NewServer(openStore(t), fixedClock(testNow), "")

	for _, repo := range []string{"", "acme", "acme/a/b", "acme/ x"} {
		if rec := postTrack(t, server, repo, true); rec.Code != http.StatusBadRequest {
			t.Errorf("Track %q = %d, want 400", repo, rec.Code)
		}
	}
}

func TestBannerPollsUntilTheStateChangesThenAsksForARefresh(t *testing.T) {
	st := openStore(t)
	server := web.NewServer(st, fixedClock(testNow), "")
	postTrack(t, server, "acme/new", true)

	stale := get(t, server, "/features/banner?repo=acme/new&seen=cloning")
	if stale.Header().Get("HX-Refresh") != "" || !strings.Contains(stale.Body.String(), `hx-trigger="every 2s"`) {
		t.Errorf("a cloning banner seen as cloning must poll without a refresh:\n%s", stale.Body)
	}

	repo := storepkg.Repo{
		Name: "acme/new", Remote: "git@github.com:acme/new.git", State: storepkg.RepoReady, TrackedAt: testNow,
	}
	if err := st.UpsertRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	consumeTrack(t, st)

	changed := get(t, server, "/features/banner?repo=acme/new&seen=cloning")
	if changed.Header().Get("HX-Refresh") != "true" {
		t.Errorf("a banner whose state moved from cloning must send HX-Refresh: true")
	}
}

func consumeTrack(t *testing.T, st *storepkg.Store) {
	t.Helper()
	intents, err := st.PendingVerbIntents(t.Context(), storepkg.TrackVerb)
	if err != nil {
		t.Fatal(err)
	}
	for _, intent := range intents {
		if err := st.ConsumeVerbIntent(t.Context(), intent.ID, testNow); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBannerGoldens(t *testing.T) {
	repo := func(name string, state storepkg.RepoState, kind, reason, source string) storepkg.Repo {
		return storepkg.Repo{
			Name: name, Remote: "git@github.com:" + name + ".git", State: state, RefusalKind: kind, Refusal: reason,
			SettingsSource: source, TrackedAt: testNow,
		}
	}
	st := openStore(t)
	for _, r := range []storepkg.Repo{
		repo("acme/cloning", storepkg.RepoCloning, "", "", ""),
		repo("acme/file", storepkg.RepoReady, "", "", "origin/main"),
		repo("acme/defaults", storepkg.RepoReady, "", "", "defaults"),
		repo("acme/merges", storepkg.RepoRefused, plan.RefusalMergeSettings, "repo acme/merges allows merge commits", ""),
		repo("acme/master", storepkg.RepoRefused, plan.RefusalDefaultBranch, "default branch is `master`, not `main`", ""),
		repo("acme/noclone", storepkg.RepoRefused, plan.RefusalClone, "clone failed", ""),
		repo("acme/badtoml", storepkg.RepoRefused, plan.RefusalSettingsParse, "toml: line 3", ""),
	} {
		if err := st.UpsertRepo(t.Context(), r); err != nil {
			t.Fatal(err)
		}
	}
	server := web.NewServer(st, fixedClock(testNow), "")
	if err := st.QueueVerbIntent(t.Context(), "acme/queued", storepkg.TrackVerb, testNow); err != nil {
		t.Fatal(err)
	}

	for name, repo := range map[string]string{
		"banner_untracked":      "acme/unknown",
		"banner_pending":        "acme/queued",
		"banner_cloning":        "acme/cloning",
		"banner_ready_file":     "acme/file",
		"banner_ready_defaults": "acme/defaults",
		"banner_refused_merge":  "acme/merges",
		"banner_refused_branch": "acme/master",
		"banner_refused_clone":  "acme/noclone",
		"banner_refused_toml":   "acme/badtoml",
	} {
		rec := get(t, server, "/features/banner?repo="+repo)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET banner %s: %d: %s", repo, rec.Code, rec.Body)
		}
		assertGolden(t, "testdata/"+name+".golden.html", rec.Body.Bytes())
	}
}
