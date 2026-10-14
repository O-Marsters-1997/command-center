package store_test

import (
	"strings"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func requireErrorNaming(t *testing.T, err error, column, value string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want one naming %s and %q", column, value)
	}
	message := err.Error() //nolint:forbidigo // the contract under test is that the text names column and value
	if !strings.Contains(message, column) || !strings.Contains(message, value) {
		t.Errorf("error = %q, want it to name %s and %q", err, column, value)
	}
}

func TestReadingARunKindOutsideTheSetNamesTheColumnAndValue(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)
	if _, err := store.InsertRunSkeleton(ctx, "sandbox://CC-1", plan.RunKind("bogus"), "base", "hash"); err != nil {
		t.Fatal(err)
	}

	_, err := store.LatestRunsByTicket(ctx)

	requireErrorNaming(t, err, "runs.kind", "bogus")
}

func TestReadingAnIntentVerbOutsideTheSetNamesTheColumnAndValue(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)
	if err := store.QueueVerbIntent(ctx, "sandbox://CC-1", plan.Verb("bogus"), time.Now()); err != nil {
		t.Fatal(err)
	}

	_, err := store.PendingIntentsByTicket(ctx)

	requireErrorNaming(t, err, "intents.verb", "bogus")
}

func TestReadingARefusalKindOutsideTheSetNamesTheColumnAndValue(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	repo := storepkg.Repo{
		Name: "acme/cc", Remote: "git@github.com:acme/cc.git", Phase: plan.RepoRefused,
		RefusalKind: plan.RefusalKind("bogus"), Refusal: "why", TrackedAt: trackedAt,
	}
	if err := store.UpsertRepo(ctx, repo); err != nil {
		t.Fatal(err)
	}

	_, err := store.Repos(ctx)

	requireErrorNaming(t, err, "repos.refusal_kind", "bogus")
}

func TestReadingAnEventKindOutsideTheSetNamesTheColumnAndValue(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	store := openStore(t)
	if err := store.AppendEvent(ctx, storepkg.Event{At: time.Now(), Kind: storepkg.EventKind("bogus")}); err != nil {
		t.Fatal(err)
	}

	_, err := store.Events(ctx)

	requireErrorNaming(t, err, "events.kind", "bogus")
}

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
