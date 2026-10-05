package cc

import (
	"context"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
)

func TestKnownRepoRowsAreReadyAndLinkToTheirScopedPage(t *testing.T) {
	t.Parallel()

	repos := []Repo{{Name: "command-center", Remote: "git@github.com:acme/command-center.git"}}
	got := knownRepoRows(repos)
	if len(got) != 1 {
		t.Fatalf("knownRepoRows = %+v, want 1 row", got)
	}
	row := got[0]
	if !row.Tracked || row.State != "ready" || row.Tone != "done" {
		t.Errorf("row = %+v, want Tracked=true, State=ready, Tone=done", row)
	}
	if row.ScopePath != "/features?repo=command-center" {
		t.Errorf("ScopePath = %q, want /features?repo=command-center", row.ScopePath)
	}
}

func TestFilterPushableMatchesCaseInsensitiveSubstring(t *testing.T) {
	t.Parallel()

	all := []gh.RepoSummary{{FullName: "acme/Alpha"}, {FullName: "acme/beta"}}
	got := filterPushable(all, "ALPHA")
	if len(got) != 1 || got[0].FullName != "acme/Alpha" {
		t.Errorf("filterPushable(_, ALPHA) = %+v, want only acme/Alpha", got)
	}
}

func TestMergeRepoRowsMatchesByRemoteNotName(t *testing.T) {
	t.Parallel()

	known := []Repo{{Name: "command-center", Remote: "git@github.com:acme/command-center.git"}}
	pushable := []gh.RepoSummary{
		{FullName: "acme/command-center", SSHURL: "git@github.com:acme/command-center.git"},
		{FullName: "acme/widgets", SSHURL: "git@github.com:acme/widgets.git"},
	}

	got := mergeRepoRows(known, pushable)
	if len(got) != 2 {
		t.Fatalf("mergeRepoRows = %+v, want 2 rows", got)
	}
	if got[0].Name != "command-center" || !got[0].Tracked || got[0].ScopePath != "/features?repo=command-center" {
		t.Errorf("known match = %+v, want the known repo's own identity", got[0])
	}
	if got[1].Name != "acme/widgets" || got[1].Tracked || got[1].State != "not tracked" {
		t.Errorf("unknown match = %+v, want a not-tracked row keyed by its full name", got[1])
	}
	if got[1].ScopePath != "/features?repo=acme/widgets" {
		t.Errorf("ScopePath = %q, want /features?repo=acme/widgets", got[1].ScopePath)
	}
}

func TestRepoNamedFindsAConfiguredRepoByItsOwnName(t *testing.T) {
	t.Parallel()

	repos := []Repo{{Name: "alpha"}, {Name: "beta"}}
	if _, ok := repoNamed(repos, "beta"); !ok {
		t.Error("repoNamed(repos, beta) = not found, want found")
	}
	if _, ok := repoNamed(repos, "gamma"); ok {
		t.Error("repoNamed(repos, gamma) = found, want not found")
	}
}

func TestPushableReposCacheFetchesOnceWithinTheTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	clock := now
	calls := 0
	fetch := func(context.Context) ([]gh.RepoSummary, error) {
		calls++
		return []gh.RepoSummary{{FullName: "acme/alpha"}}, nil
	}
	cache := newPushableReposCache(fetch, func() time.Time { return clock })

	ctx := t.Context()
	if _, err := cache.Get(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Get(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1: repeated reads inside the TTL must not refetch", calls)
	}

	clock = now.Add(pushableReposTTL)
	if _, err := cache.Get(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2: a read at the TTL boundary must refetch", calls)
	}
}
