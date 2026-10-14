package store_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestReadingARowWithNullRefusalKindDecodesToUnknown(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	repo := storepkg.Repo{
		Name: "acme/cc", Remote: "git@github.com:acme/cc.git", Phase: plan.RepoReady, TrackedAt: trackedAt,
	}
	if err := store.UpsertRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}

	repos, err := store.Repos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].RefusalKind != plan.RefusalUnknown {
		t.Errorf("Repos() = %+v, want one repo with RefusalUnknown", repos)
	}
}
