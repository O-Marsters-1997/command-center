package loop_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func repoByName(t *testing.T, st *storepkg.Store, name string) storepkg.Repo {
	t.Helper()
	repos, err := st.Repos(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, repo := range repos {
		if repo.Name == name {
			return repo
		}
	}
	t.Fatalf("repo %s is not tracked: %+v", name, repos)
	return storepkg.Repo{}
}

func upsertRepoAs(t *testing.T, st *storepkg.Store, name string, state storepkg.RepoState) {
	t.Helper()
	repo := storepkg.Repo{Name: name, Remote: "git@github.com:" + name + ".git", State: state, TrackedAt: testAt}
	if state == storepkg.RepoRefused {
		repo.RefusalKind, repo.Refusal = plan.RefusalMergeSettings, "allows merge commits"
	}
	if err := st.UpsertRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
}

func TestTheFirstTickSettlesEachCloningRepoIntoReadyOrRefused(t *testing.T) {
	st := openStore(t)
	upsertRepoAs(t, st, "acme/good", storepkg.RepoCloning)
	upsertRepoAs(t, st, "acme/merges", storepkg.RepoCloning)
	cfg, ws := testConfigAndWorkspace(t, t.TempDir(), 1, []string{"true"})
	idle := func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil }
	lp := loop.NewLoop(st, idle, fixedClock(testAt), cfg, ws, runner.ProcessRunner{})
	lp.SetValidator(func(_ context.Context, _ string, repo storepkg.Repo, now time.Time) (storepkg.Repo, error) {
		if repo.Name == "acme/merges" {
			repo.State, repo.RefusalKind, repo.Refusal = storepkg.RepoRefused, plan.RefusalMergeSettings, "allows merge commits"
			return repo, nil
		}
		repo.State, repo.SettingsSource, repo.SettingsReadAt = storepkg.RepoReady, "defaults", now
		return repo, nil
	})

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	good := repoByName(t, st, "acme/good")
	if good.State != storepkg.RepoReady || good.SettingsSource != "defaults" || !good.SettingsReadAt.Equal(testAt) {
		t.Errorf("acme/good = %+v, want ready with its settings read recorded", good)
	}
	merges := repoByName(t, st, "acme/merges")
	if merges.State != storepkg.RepoRefused || merges.RefusalKind != plan.RefusalMergeSettings {
		t.Errorf("acme/merges = %+v, want refused(merge_settings)", merges)
	}
	lastErr, found, err := st.LastError(t.Context())
	if err != nil || !found || !strings.Contains(lastErr.Message, "acme/merges") {
		t.Errorf("last error = %+v (found %v, err %v), want one naming acme/merges", lastErr, found, err)
	}
}

func TestAValidatorThatCannotAnswerLeavesTheRepoCloningAndTheOthersSettle(t *testing.T) {
	st := openStore(t)
	upsertRepoAs(t, st, "acme/offline", storepkg.RepoCloning)
	upsertRepoAs(t, st, "acme/good", storepkg.RepoCloning)
	cfg, ws := testConfigAndWorkspace(t, t.TempDir(), 1, []string{"true"})
	idle := func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil }
	lp := loop.NewLoop(st, idle, fixedClock(testAt), cfg, ws, runner.ProcessRunner{})
	lp.SetValidator(func(_ context.Context, _ string, repo storepkg.Repo, _ time.Time) (storepkg.Repo, error) {
		if repo.Name == "acme/offline" {
			return repo, os.ErrDeadlineExceeded
		}
		repo.State = storepkg.RepoReady
		return repo, nil
	})

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := repoByName(t, st, "acme/offline").State; got != storepkg.RepoCloning {
		t.Errorf("acme/offline state = %s, want cloning", got)
	}
	if got := repoByName(t, st, "acme/good").State; got != storepkg.RepoReady {
		t.Errorf("acme/good state = %s, want ready", got)
	}
}

func pathWithFakeGh(t *testing.T, apiJSON string) {
	t.Helper()
	bin := t.TempDir()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s' '" + apiJSON + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestValidateRepoRefusesAMasterDefaultBeforeCloningIt(t *testing.T) {
	pathWithFakeGh(t, `{"allow_merge_commit":false,"allow_rebase_merge":false,"default_branch":"master"}`)
	dataDir := t.TempDir()
	repo := storepkg.Repo{Name: "acme/old", Remote: "/nowhere/remote.git", State: storepkg.RepoCloning}

	got, err := loop.ValidateRepo(t.Context(), dataDir, repo, testAt)
	if err != nil {
		t.Fatalf("ValidateRepo: %v", err)
	}

	wantReason := "default branch is `master`, not `main`; rename it on GitHub, then Track again."
	if got.State != storepkg.RepoRefused || got.RefusalKind != plan.RefusalDefaultBranch || got.Refusal != wantReason {
		t.Errorf("ValidateRepo = %+v, want refused(default_branch) with %q", got, wantReason)
	}
	if _, err := os.Stat(config.CheckoutPath(dataDir, repo.Name)); !os.IsNotExist(err) {
		t.Errorf("checkout stat = %v, want it never cloned", err)
	}
}

func TestValidateRepoClonesAcceptedReposAndReadsTheirSettings(t *testing.T) {
	root, _ := repoWithOrigin(t)
	pathWithFakeGh(t, `{"allow_merge_commit":false,"allow_rebase_merge":false,"default_branch":"main"}`)
	dataDir := t.TempDir()
	repo := storepkg.Repo{Name: "acme/cc", Remote: filepath.Join(root, "remote.git"), State: storepkg.RepoCloning}

	got, err := loop.ValidateRepo(t.Context(), dataDir, repo, testAt)
	if err != nil {
		t.Fatalf("ValidateRepo: %v", err)
	}

	if got.State != storepkg.RepoReady || got.SettingsSource != string(config.SourceDefaults) ||
		!got.SettingsReadAt.Equal(testAt) {
		t.Errorf("ValidateRepo = %+v, want ready, read from defaults at %v", got, testAt)
	}
	if _, err := os.Stat(filepath.Join(config.CheckoutPath(dataDir, repo.Name), "README.md")); err != nil {
		t.Errorf("checkout was not cloned: %v", err)
	}
}

func TestValidateRepoRefusesARepoWhoseSettingsFileDoesNotParse(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	pushSettingsFile(t, repoPath, "stackin = true\n")
	pathWithFakeGh(t, `{"allow_merge_commit":false,"allow_rebase_merge":false,"default_branch":"main"}`)
	repo := storepkg.Repo{Name: "acme/cc", Remote: filepath.Join(root, "remote.git"), State: storepkg.RepoCloning}

	got, err := loop.ValidateRepo(t.Context(), t.TempDir(), repo, testAt)
	if err != nil {
		t.Fatalf("ValidateRepo: %v", err)
	}

	if got.State != storepkg.RepoRefused || got.RefusalKind != plan.RefusalSettingsParse {
		t.Errorf("ValidateRepo = %+v, want refused(settings_parse)", got)
	}
}

func TestBreakingTheSettingsFileRefusesTheRepoWithinATickAndFixingItHeals(t *testing.T) {
	root, repoPath := repoWithOrigin(t)
	pathWithGitAndTpOnly(t)
	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	st := openStore(t)
	trackRepo(t, st, "repo", root+"/remote.git")
	ticket := sandboxTicket("1")
	if err := st.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
		t.Fatal(err)
	}
	observer := loop.NewObserver(st, &fakeForge{t: t, remote: root + "/remote.git"}, cfg)
	lp := loop.NewLoop(st, observer, fixedClock(testAt), cfg, ws, runner.ProcessRunner{})
	tick := func() storepkg.Repo {
		t.Helper()
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		return repoByName(t, st, "repo")
	}

	if got := tick(); got.State != storepkg.RepoReady || got.SettingsSource != string(config.SourceDefaults) {
		t.Fatalf("repo after a clean tick = %+v, want ready read from defaults", got)
	}

	pushSettingsFile(t, repoPath, "stackin = true\n")
	got := tick()
	if got.State != storepkg.RepoRefused || got.RefusalKind != plan.RefusalSettingsParse ||
		!strings.Contains(got.Refusal, "stackin") {
		t.Fatalf("repo after the file broke = %+v, want refused(settings_parse) naming the key", got)
	}
	tickets, err := st.Tickets(t.Context())
	if err != nil || len(tickets) != 1 {
		t.Fatalf("tickets = %+v (err %v), want the refused repo's ticket still on the board", tickets, err)
	}

	pushSettingsFile(t, repoPath, "stacking = true\n")
	if got := tick(); got.State != storepkg.RepoReady || got.Refusal != "" || got.SettingsSource != string(config.SourceFile) {
		t.Errorf("repo after the file was fixed = %+v, want ready again read from origin/main", got)
	}
}

func TestARefusedRepoOtherThanSettingsParseWaitsForTrackEvenWithCleanSettings(t *testing.T) {
	root, _ := repoWithOrigin(t)
	pathWithGitAndTpOnly(t)
	cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
	st := openStore(t)
	upsertRepoAs(t, st, "repo", storepkg.RepoRefused)
	observer := loop.NewObserver(st, &fakeForge{t: t, remote: root + "/remote.git"}, cfg)
	lp := loop.NewLoop(st, observer, fixedClock(testAt), cfg, ws, runner.ProcessRunner{})

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if got := repoByName(t, st, "repo"); got.State != storepkg.RepoRefused || got.RefusalKind != plan.RefusalMergeSettings {
		t.Errorf("repo = %+v, want it still refused(merge_settings)", got)
	}
}

func TestOneReposFetchFailureLeavesTheOthersObserved(t *testing.T) {
	root, _ := repoWithOrigin(t)
	pathWithGitAndTpOnly(t)
	if err := os.MkdirAll(config.CheckoutPath(root, "acme/broken"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, _ := testConfigAndWorkspace(t, root, 1, []string{"true"})
	st := openStore(t)
	trackRepo(t, st, "repo", root+"/remote.git")
	upsertRepoAs(t, st, "acme/broken", storepkg.RepoReady)
	brokenPR := plan.BranchKey("acme/broken", "cc-1")
	previous := plan.Observation{PRs: map[string]plan.PR{brokenPR: {Number: 7, State: plan.Open}}}
	if err := st.SaveObservation(t.Context(), previous); err != nil {
		t.Fatal(err)
	}
	observer := loop.NewObserver(st, &fakeForge{t: t, remote: root + "/remote.git"}, cfg)

	obs, err := observer(t.Context())
	if err != nil {
		t.Fatalf("observe: %v", err)
	}

	if obs.PRs[brokenPR].Number != 7 {
		t.Errorf("PRs = %+v, want acme/broken to keep its last observed pull request", obs.PRs)
	}

	if _, ok := obs.Settings["repo"]; !ok {
		t.Errorf("settings = %+v, want repo observed despite acme/broken failing", obs.Settings)
	}
	if obs.RepoErrors["acme/broken"] == "" || len(obs.RepoErrors) != 1 {
		t.Errorf("repo errors = %+v, want exactly acme/broken", obs.RepoErrors)
	}
}
