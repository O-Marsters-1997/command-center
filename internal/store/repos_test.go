package store_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

var trackedAt = time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)

func cloningRepo(t *testing.T, store *storepkg.Store) storepkg.Repo {
	t.Helper()
	repo := storepkg.Repo{
		Name: "acme/cc", Remote: "git@github.com:acme/cc.git", Phase: plan.RepoCloning, TrackedAt: trackedAt,
	}
	if err := store.UpsertRepo(t.Context(), repo); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}
	return repo
}

func TestStore_SetRepoState(t *testing.T) {
	t.Parallel()

	t.Run("records a refusal", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		repo := cloningRepo(t, store)
		repo.Phase, repo.RefusalKind, repo.Refusal = plan.RepoRefused, plan.RefusalMergeSettings, "merge commits allowed"
		if err := store.SetRepoState(t.Context(), repo); err != nil {
			t.Fatalf("SetRepoState: %v", err)
		}
		if diff := cmp.Diff([]storepkg.Repo{repo}, readRepos(t, store)); diff != "" {
			t.Errorf("Repos() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("records the settings read", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		repo := cloningRepo(t, store)
		repo.Phase, repo.SettingsSource, repo.SettingsReadAt = plan.RepoReady, "origin/main", trackedAt.Add(time.Hour)
		if err := store.SetRepoState(t.Context(), repo); err != nil {
			t.Fatalf("SetRepoState: %v", err)
		}
		if diff := cmp.Diff([]storepkg.Repo{repo}, readRepos(t, store)); diff != "" {
			t.Errorf("Repos() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a refusal without a reason fails the check constraint", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		repo := cloningRepo(t, store)
		repo.Phase = plan.RepoRefused
		if err := store.SetRepoState(t.Context(), repo); err == nil {
			t.Error("SetRepoState(refused, no refusal) error = nil, want the check constraint to refuse it")
		}
	})
}

func TestStore_Repos(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	if err := store.UpsertRepo(t.Context(), storepkg.Repo{
		Name: "acme/cc", Remote: "git@github.com:acme/cc.git", Phase: plan.RepoReady, TrackedAt: trackedAt,
	}); err != nil {
		t.Fatalf("UpsertRepo: %v", err)
	}
	repos := readRepos(t, store)
	if len(repos) != 1 || repos[0].RefusalKind != plan.RefusalUnknown {
		t.Errorf("Repos() with a NULL refusal kind = %+v, want one repo with RefusalUnknown", repos)
	}
}
