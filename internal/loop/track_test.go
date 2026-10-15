package loop_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh/ghtest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tp/tptest"
)

func repoByName(t *testing.T, st *storepkg.Store, name string) storepkg.Repo {
	t.Helper()
	repos, err := st.Repos(t.Context())
	if err != nil {
		t.Fatalf("Repos(): %v", err)
	}
	for _, repo := range repos {
		if repo.Name == name {
			return repo
		}
	}
	t.Fatalf("Repos() has no %s: %+v", name, repos)
	return storepkg.Repo{}
}

func upsertRepoAs(t *testing.T, st *storepkg.Store, name string, state plan.RepoPhase) {
	t.Helper()
	repo := storepkg.Repo{Name: name, Remote: "git@github.com:" + name + ".git", Phase: state, TrackedAt: testAt}
	if state == plan.RepoRefused {
		repo.RefusalKind, repo.Refusal = plan.RefusalMergeSettings, "allows merge commits"
	}
	if err := st.UpsertRepo(t.Context(), repo); err != nil {
		t.Fatalf("UpsertRepo(%q): %v", name, err)
	}
}

func validatingLoop(t *testing.T, st *storepkg.Store, validate loop.ValidateFunc, opts ...loop.Option) *loop.Loop {
	t.Helper()
	cfg, ws := testConfigAndWorkspace(t, t.TempDir(), 1, []string{"true"})
	remoteOf := func(_ context.Context, name string) (string, error) {
		if name == "acme/missing" {
			return "", errors.New("gh: Not Found")
		}
		return "git@github.com:" + name + ".git", nil
	}
	opts = append([]loop.Option{loop.WithRemoteSource(remoteOf), loop.WithValidator(validate)}, opts...)
	return newLoop(st, noOpObserve, fixedClock(testAt), cfg, ws, runner.ProcessRunner{}, opts...)
}

func readyValidator(validations *[]string) loop.ValidateFunc {
	return func(_ context.Context, _ string, repo storepkg.Repo, now time.Time) (storepkg.Repo, error) {
		*validations = append(*validations, repo.Name)
		repo.Phase, repo.RefusalKind, repo.Refusal = plan.RepoReady, "", ""
		repo.SettingsSource, repo.SettingsReadAt = "defaults", now
		return repo, nil
	}
}

func queueTrack(t *testing.T, st *storepkg.Store, repo string) {
	t.Helper()
	if _, err := st.QueueTrackIntent(t.Context(), repo, testAt); err != nil {
		t.Fatalf("QueueTrackIntent(%q): %v", repo, err)
	}
}

func runOnce(t *testing.T, lp *loop.Loop) {
	t.Helper()
	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
}

func TestTrack(t *testing.T) {
	t.Parallel()

	t.Run("adds the repo and validates it in the same tick", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		var validations []string
		queueTrack(t, st, "acme/new")

		runOnce(t, validatingLoop(t, st, readyValidator(&validations)))

		got := repoByName(t, st, "acme/new")
		if got.Phase != plan.RepoReady || got.Remote != "git@github.com:acme/new.git" || got.SettingsSource != "defaults" {
			t.Errorf("acme/new = %+v, want ready with the remote gh gave", got)
		}
		if pending, err := st.TrackPending(t.Context(), "acme/new"); err != nil || pending {
			t.Errorf("TrackPending(acme/new) = %v, %v, want false, nil", pending, err)
		}
	})

	t.Run("revalidates a refused repo and leaves a ready one alone", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		upsertRepoAs(t, st, "acme/refused", plan.RepoRefused)
		upsertRepoAs(t, st, "acme/ready", plan.RepoReady)
		var validations []string
		queueTrack(t, st, "acme/refused")
		queueTrack(t, st, "acme/ready")

		runOnce(t, validatingLoop(t, st, readyValidator(&validations)))

		if diff := cmp.Diff([]string{"acme/refused"}, validations); diff != "" {
			t.Errorf("validated repos mismatch (-want +got):\n%s", diff)
		}
		if got := repoByName(t, st, "acme/refused"); got.Phase != plan.RepoReady || got.RefusalKind != "" {
			t.Errorf("acme/refused = %+v, want ready with the refusal cleared", got)
		}
	})

	t.Run("refuses a repo GitHub does not know without validating it", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		var validations []string
		queueTrack(t, st, "acme/missing")

		runOnce(t, validatingLoop(t, st, readyValidator(&validations)))

		got := repoByName(t, st, "acme/missing")
		if got.Phase != plan.RepoRefused || got.RefusalKind != plan.RefusalClone || len(validations) != 0 {
			t.Errorf("acme/missing = %+v (validated %v), want refused(clone) and never validated", got, validations)
		}
	})

	t.Run("the first tick settles each cloning repo into ready or refused", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		upsertRepoAs(t, st, "acme/good", plan.RepoCloning)
		upsertRepoAs(t, st, "acme/merges", plan.RepoCloning)
		validate := func(_ context.Context, _ string, repo storepkg.Repo, now time.Time) (storepkg.Repo, error) {
			if repo.Name == "acme/merges" {
				repo.Phase, repo.RefusalKind, repo.Refusal = plan.RepoRefused, plan.RefusalMergeSettings, "allows merge commits"
				return repo, nil
			}
			repo.Phase, repo.SettingsSource, repo.SettingsReadAt = plan.RepoReady, "defaults", now
			return repo, nil
		}

		runOnce(t, validatingLoop(t, st, validate))

		if good := repoByName(t, st, "acme/good"); good.Phase != plan.RepoReady || !good.SettingsReadAt.Equal(testAt) {
			t.Errorf("acme/good = %+v, want ready with its settings read at %v", good, testAt)
		}
		if merges := repoByName(t, st, "acme/merges"); merges.Phase != plan.RepoRefused ||
			merges.RefusalKind != plan.RefusalMergeSettings {
			t.Errorf("acme/merges = %+v, want refused(merge_settings)", merges)
		}
		lastErr, err := st.LastError(t.Context())
		if err != nil || lastErr.IsNone() || !strings.Contains(lastErr.Unwrap().Message, "acme/merges") {
			t.Errorf("LastError() = %v, %v, want one naming acme/merges", lastErr, err)
		}
	})

	t.Run("a validator that cannot answer leaves its repo cloning and the others settle", func(t *testing.T) {
		t.Parallel()
		st := openStore(t)
		upsertRepoAs(t, st, "acme/offline", plan.RepoCloning)
		upsertRepoAs(t, st, "acme/good", plan.RepoCloning)
		validate := func(_ context.Context, _ string, repo storepkg.Repo, _ time.Time) (storepkg.Repo, error) {
			if repo.Name == "acme/offline" {
				return repo, os.ErrDeadlineExceeded
			}
			repo.Phase = plan.RepoReady
			return repo, nil
		}

		runOnce(t, validatingLoop(t, st, validate))

		got := [2]plan.RepoPhase{repoByName(t, st, "acme/offline").Phase, repoByName(t, st, "acme/good").Phase}
		if want := [2]plan.RepoPhase{plan.RepoCloning, plan.RepoReady}; got != want {
			t.Errorf("phases of acme/offline, acme/good = %v, want %v", got, want)
		}
	})

	t.Run("a repo refused for other than settings_parse waits for Track even with clean settings", func(t *testing.T) {
		t.Parallel()
		root, _ := repoWithOrigin(t)
		cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
		st := openStore(t)
		upsertRepoAs(t, st, "repo", plan.RepoRefused)
		forge := &ghtest.Forge{}
		runOnce(t, newLoop(st, loop.NewObserver(st, forge, cfg), fixedClock(testAt), cfg, ws, runner.ProcessRunner{},
			loop.WithForge(forge), loop.WithWorktrees(&tptest.Worktrees{})))

		if got := repoByName(t, st, "repo"); got.Phase != plan.RepoRefused || got.RefusalKind != plan.RefusalMergeSettings {
			t.Errorf("repo = %+v, want still refused(merge_settings)", got)
		}
	})

	t.Run("breaking the settings file refuses the repo within a tick and fixing it heals", func(t *testing.T) {
		t.Parallel()
		root, repoPath := repoWithOrigin(t)
		cfg, ws := testConfigAndWorkspace(t, root, 1, []string{"true"})
		st := openStore(t)
		trackRepo(t, st, "repo", root+"/remote.git")
		ticket := sandboxTicket("1")
		if err := st.UpsertTickets(t.Context(), []storepkg.Ticket{ticket}); err != nil {
			t.Fatalf("UpsertTickets: %v", err)
		}
		forge := &ghtest.Forge{}
		lp := newLoop(st, loop.NewObserver(st, forge, cfg), fixedClock(testAt), cfg, ws, runner.ProcessRunner{},
			loop.WithForge(forge), loop.WithWorktrees(&tptest.Worktrees{}))
		tick := func() storepkg.Repo {
			t.Helper()
			runOnce(t, lp)
			return repoByName(t, st, "repo")
		}

		if got := tick(); got.Phase != plan.RepoReady || got.SettingsSource != string(config.SourceDefaults) {
			t.Fatalf("repo after a clean tick = %+v, want ready from defaults", got)
		}
		pushSettingsFile(t, repoPath, "stackin = true\n")
		if got := tick(); got.Phase != plan.RepoRefused || got.RefusalKind != plan.RefusalSettingsParse {
			t.Fatalf("repo after the file broke = %+v, want refused(settings_parse)", got)
		}
		if tickets, err := st.Tickets(t.Context()); err != nil || len(tickets) != 1 {
			t.Fatalf("Tickets() = %+v, %v, want the refused repo's ticket kept", tickets, err)
		}
		pushSettingsFile(t, repoPath, "stacking = true\n")
		if healed := tick(); healed.Phase != plan.RepoReady || healed.Refusal != "" ||
			healed.SettingsSource != string(config.SourceFile) {
			t.Errorf("repo after the fix = %+v, want ready from origin/main's file", healed)
		}
	})
}
