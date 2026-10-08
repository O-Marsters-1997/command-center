package store_test

import (
	"maps"
	"testing"
	"time"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

var trackedAt = time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)

func TestImportReposRenamesTicketsToOwnerName(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := t.Context()
	if err := store.UpsertTickets(ctx, []storepkg.Ticket{
		{URL: "https://github.com/acme/cc/issues/1", Repo: "cc", Branch: "issue-1"},
		{URL: "https://github.com/acme/other/issues/2", Repo: "other", Branch: "issue-2"},
	}); err != nil {
		t.Fatal(err)
	}

	cc := storepkg.Repo{
		Name: "acme/cc", Remote: "git@github.com:acme/cc.git", State: storepkg.RepoReady, TrackedAt: trackedAt,
	}
	if err := store.ImportRepos(ctx, []storepkg.RepoImport{{ShortName: "cc", Repo: cc}}); err != nil {
		t.Fatalf("ImportRepos: %v", err)
	}

	repos, err := store.Repos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertRepos(t, repos, cc)
	tickets, err := store.Tickets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, ticket := range tickets {
		got[ticket.URL] = ticket.Repo
	}
	want := map[string]string{
		"https://github.com/acme/cc/issues/1":    "acme/cc",
		"https://github.com/acme/other/issues/2": "other",
	}
	if !maps.Equal(got, want) {
		t.Errorf("ticket repos = %v, want %v", got, want)
	}
}

func TestSetRepoStateRecordsARefusal(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := t.Context()
	repo := storepkg.Repo{
		Name: "acme/cc", Remote: "git@github.com:acme/cc.git", State: storepkg.RepoCloning, TrackedAt: trackedAt,
	}
	if err := store.UpsertRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}

	refused := repo
	refused.State = storepkg.RepoRefused
	if err := store.SetRepoState(ctx, refused); err == nil {
		t.Error("SetRepoState(refused, no refusal) = nil, want the check constraint to refuse it")
	}
	refused.RefusalKind, refused.Refusal = "merge", "merge commits allowed"
	if err := store.SetRepoState(ctx, refused); err != nil {
		t.Fatalf("SetRepoState: %v", err)
	}

	repos, err := store.Repos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repo.State, repo.RefusalKind, repo.Refusal = storepkg.RepoRefused, "merge", "merge commits allowed"
	assertRepos(t, repos, repo)
}

func TestSetRepoStateRecordsTheSettingsRead(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := t.Context()
	repo := storepkg.Repo{
		Name: "acme/cc", Remote: "git@github.com:acme/cc.git", State: storepkg.RepoCloning, TrackedAt: trackedAt,
	}
	if err := store.UpsertRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}

	repo.State, repo.SettingsSource, repo.SettingsReadAt = storepkg.RepoReady, "origin/main", trackedAt.Add(time.Hour)
	if err := store.SetRepoState(ctx, repo); err != nil {
		t.Fatalf("SetRepoState: %v", err)
	}

	repos, err := store.Repos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].State != storepkg.RepoReady || repos[0].SettingsSource != "origin/main" ||
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
