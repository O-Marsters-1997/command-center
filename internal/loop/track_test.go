package loop_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/runner"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func trackLoop(t *testing.T, st *storepkg.Store, validations *[]string) *loop.Loop {
	t.Helper()
	cfg, ws := testConfigAndWorkspace(t, t.TempDir(), 1, []string{"true"})
	idle := func(context.Context) (plan.Observation, error) { return plan.Observation{}, nil }
	lp := loop.NewLoop(st, idle, fixedClock(testAt), cfg, ws, runner.ProcessRunner{})
	lp.SetRemoteSource(func(_ context.Context, name string) (string, error) {
		if name == "acme/missing" {
			return "", errors.New("gh: Not Found")
		}
		return "git@github.com:" + name + ".git", nil
	})
	lp.SetValidator(func(_ context.Context, _ string, repo storepkg.Repo, now time.Time) (storepkg.Repo, error) {
		*validations = append(*validations, repo.Name)
		repo.State, repo.RefusalKind, repo.Refusal = storepkg.RepoReady, "", ""
		repo.SettingsSource, repo.SettingsReadAt = "defaults", now
		return repo, nil
	})
	return lp
}

func queueTrack(t *testing.T, st *storepkg.Store, repo string) {
	t.Helper()
	if _, err := st.QueueTrackIntent(t.Context(), repo, testAt); err != nil {
		t.Fatal(err)
	}
}

func TestATrackIntentAddsTheRepoAndValidatesItInTheSameTick(t *testing.T) {
	st := openStore(t)
	var validations []string
	lp := trackLoop(t, st, &validations)
	queueTrack(t, st, "acme/new")

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got := repoByName(t, st, "acme/new")
	if got.State != storepkg.RepoReady || got.Remote != "git@github.com:acme/new.git" || got.SettingsSource != "defaults" {
		t.Errorf("acme/new = %+v, want ready with the remote gh gave", got)
	}
	if pending, err := st.TrackPending(t.Context(), "acme/new"); err != nil || pending {
		t.Errorf("track still pending (%v, err %v) after the tick", pending, err)
	}
}

func TestTrackOnARefusedRepoValidatesItAgainAndOnAReadyRepoDoesNothing(t *testing.T) {
	st := openStore(t)
	upsertRepoAs(t, st, "acme/refused", storepkg.RepoRefused)
	upsertRepoAs(t, st, "acme/ready", storepkg.RepoReady)
	var validations []string
	lp := trackLoop(t, st, &validations)
	queueTrack(t, st, "acme/refused")
	queueTrack(t, st, "acme/ready")

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if len(validations) != 1 || validations[0] != "acme/refused" {
		t.Errorf("validated %v, want only acme/refused", validations)
	}
	if got := repoByName(t, st, "acme/refused"); got.State != storepkg.RepoReady || got.RefusalKind != "" {
		t.Errorf("acme/refused = %+v, want ready with the refusal cleared", got)
	}
}

func TestTrackOfARepoGitHubDoesNotKnowRefusesItForTrackAgain(t *testing.T) {
	st := openStore(t)
	var validations []string
	lp := trackLoop(t, st, &validations)
	queueTrack(t, st, "acme/missing")

	if err := lp.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got := repoByName(t, st, "acme/missing")
	if got.State != storepkg.RepoRefused || got.RefusalKind != plan.RefusalClone || len(validations) != 0 {
		t.Errorf("acme/missing = %+v (validated %v), want refused(clone) and never validated", got, validations)
	}
}
