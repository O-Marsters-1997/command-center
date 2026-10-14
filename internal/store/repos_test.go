package store_test

import (
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

var trackedAt = time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)

func TestSetRepoStateRecordsARefusal(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := t.Context()
	repo := storepkg.Repo{
		Name: "acme/cc", Remote: "git@github.com:acme/cc.git", Phase: plan.RepoCloning, TrackedAt: trackedAt,
	}
	if err := store.UpsertRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}

	refused := repo
	refused.Phase = plan.RepoRefused
	if err := store.SetRepoState(ctx, refused); err == nil {
		t.Error("SetRepoState(refused, no refusal) = nil, want the check constraint to refuse it")
	}
	refused.RefusalKind, refused.Refusal = plan.RefusalMergeSettings, "merge commits allowed"
	if err := store.SetRepoState(ctx, refused); err != nil {
		t.Fatalf("SetRepoState: %v", err)
	}

	repos, err := store.Repos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repo.Phase, repo.RefusalKind, repo.Refusal = plan.RepoRefused, plan.RefusalMergeSettings, "merge commits allowed"
	assertRepos(t, repos, repo)
}

func TestSetRepoStateRecordsTheSettingsRead(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := t.Context()
	repo := storepkg.Repo{
		Name: "acme/cc", Remote: "git@github.com:acme/cc.git", Phase: plan.RepoCloning, TrackedAt: trackedAt,
	}
	if err := store.UpsertRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}

	repo.Phase, repo.SettingsSource, repo.SettingsReadAt = plan.RepoReady, "origin/main", trackedAt.Add(time.Hour)
	if err := store.SetRepoState(ctx, repo); err != nil {
		t.Fatalf("SetRepoState: %v", err)
	}

	repos, err := store.Repos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Phase != plan.RepoReady || repos[0].SettingsSource != "origin/main" ||
		!repos[0].SettingsReadAt.Equal(repo.SettingsReadAt) {
		t.Errorf("Repos() = %+v, want one ready repo read from origin/main at %v", repos, repo.SettingsReadAt)
	}
}

func assertRepos(t *testing.T, got []storepkg.Repo, want ...storepkg.Repo) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Repos() = %+v, want %+v", got, want)
	}
	for i := range want {
		if !got[i].TrackedAt.Equal(want[i].TrackedAt) {
			t.Errorf("Repos()[%d].TrackedAt = %v, want %v", i, got[i].TrackedAt, want[i].TrackedAt)
		}
		got[i].TrackedAt = want[i].TrackedAt
		if got[i] != want[i] {
			t.Errorf("Repos()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
