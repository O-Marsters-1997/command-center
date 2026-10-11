package store_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func trackRepos(t *testing.T, store *storepkg.Store, names ...string) {
	t.Helper()
	for _, name := range names {
		repo := storepkg.Repo{
			Name:      name,
			Remote:    "git@github.com:" + name + ".git",
			State:     storepkg.RepoReady,
			TrackedAt: trackedAt,
		}
		if err := store.UpsertRepo(t.Context(), repo); err != nil {
			t.Fatal(err)
		}
	}
}

func assertProjects(t *testing.T, got []storepkg.Project, want ...storepkg.Project) {
	t.Helper()
	same := slices.EqualFunc(got, want, func(a, b storepkg.Project) bool {
		return a.Name == b.Name && a.Hue == b.Hue && slices.Equal(a.Repos, b.Repos)
	})
	if !same {
		t.Errorf("Projects() = %+v, want %+v", got, want)
	}
}

func TestCreateProjectReadsBackWithSortedRepos(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := t.Context()
	trackRepos(t, store, "acme/web", "acme/api", "acme/loose")

	if err := store.CreateProject(ctx, "acme", 2, []string{"acme/web", "acme/api"}, trackedAt); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := store.Projects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertProjects(t, got, storepkg.Project{Name: "acme", Hue: 2, Repos: []string{"acme/api", "acme/web"}})
	repos, err := store.Repos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	loose := repos[slices.IndexFunc(repos, func(r storepkg.Repo) bool { return r.Name == "acme/loose" })]
	if loose.Project != "" || loose.Hue != 0 {
		t.Errorf("loose repo = project %q hue %d, want none", loose.Project, loose.Hue)
	}
}

func TestCreateProjectRefusesACaseOnlyDuplicate(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := t.Context()
	trackRepos(t, store, "acme/web", "acme/api")
	if err := store.CreateProject(ctx, "acme", 1, []string{"acme/web"}, trackedAt); err != nil {
		t.Fatal(err)
	}

	err := store.CreateProject(ctx, "ACME", 1, []string{"acme/api"}, trackedAt)

	if !errors.Is(err, storepkg.ErrProjectExists) {
		t.Fatalf("CreateProject(ACME) = %v, want ErrProjectExists", err)
	}
	got, err := store.Projects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertProjects(t, got, storepkg.Project{Name: "acme", Hue: 1, Repos: []string{"acme/web"}})
}

func TestCreateProjectMovesARepoOutOfItsProject(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := t.Context()
	trackRepos(t, store, "acme/web", "acme/api")
	if err := store.CreateProject(ctx, "old", 1, []string{"acme/web", "acme/api"}, trackedAt); err != nil {
		t.Fatal(err)
	}

	if err := store.CreateProject(ctx, "new", 3, []string{"acme/web"}, trackedAt); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := store.Projects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assertProjects(t, got,
		storepkg.Project{Name: "new", Hue: 3, Repos: []string{"acme/web"}},
		storepkg.Project{Name: "old", Hue: 1, Repos: []string{"acme/api"}},
	)
}

func TestCreateProjectRefusesABadNameOrHue(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	for name, c := range map[string]struct {
		name string
		hue  int
	}{
		"empty name":  {"", 1},
		"has a space": {"a b", 1},
		"leading dot": {".acme", 1},
		"too long":    {strings.Repeat("a", 41), 1},
		"hue zero":    {"acme", 0},
		"hue four":    {"acme", 4},
	} {
		err := store.CreateProject(t.Context(), c.name, c.hue, nil, trackedAt)
		if !errors.Is(err, storepkg.ErrInvalidProject) {
			t.Errorf("%s: CreateProject(%q, %d) = %v, want ErrInvalidProject", name, c.name, c.hue, err)
		}
	}
}

func TestCreateProjectWithAnUnknownRepoCreatesNothing(t *testing.T) {
	t.Parallel()
	store := openStore(t)

	err := store.CreateProject(t.Context(), "acme", 1, []string{"acme/ghost"}, trackedAt)

	if !errors.Is(err, storepkg.ErrNotFound) {
		t.Fatalf("CreateProject = %v, want ErrNotFound", err)
	}
	got, err := store.Projects(t.Context())
	if err != nil || len(got) != 0 {
		t.Errorf("Projects() = %v, %v, want none", got, err)
	}
}

func TestSetRepoProjectMovesAndLoosensARepo(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	ctx := t.Context()
	trackRepos(t, store, "acme/web")
	if err := store.CreateProject(ctx, "one", 1, nil, trackedAt); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProject(ctx, "two", 2, nil, trackedAt); err != nil {
		t.Fatal(err)
	}

	if err := store.SetRepoProject(ctx, "acme/web", "TWO"); err != nil {
		t.Fatalf("SetRepoProject: %v", err)
	}
	repos, err := store.Repos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if repos[0].Project != "two" || repos[0].Hue != 2 {
		t.Errorf("repo = project %q hue %d, want two 2", repos[0].Project, repos[0].Hue)
	}

	if err := store.SetRepoProject(ctx, "acme/web", ""); err != nil {
		t.Fatalf("SetRepoProject(empty): %v", err)
	}
	if repos, err = store.Repos(ctx); err != nil || repos[0].Project != "" {
		t.Errorf("repo project = %q, %v, want loose", repos[0].Project, err)
	}
	if err := store.SetRepoProject(ctx, "acme/web", "nope"); !errors.Is(err, storepkg.ErrNotFound) {
		t.Errorf("SetRepoProject(unknown project) = %v, want ErrNotFound", err)
	}
	if err := store.SetRepoProject(ctx, "acme/ghost", "one"); !errors.Is(err, storepkg.ErrNotFound) {
		t.Errorf("SetRepoProject(unknown repo) = %v, want ErrNotFound", err)
	}
}
