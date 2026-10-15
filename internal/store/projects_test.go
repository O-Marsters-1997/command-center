package store_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func trackRepos(t *testing.T, store *storepkg.Store, names ...string) {
	t.Helper()
	for _, name := range names {
		repo := storepkg.Repo{
			Name:      name,
			Remote:    "git@github.com:" + name + ".git",
			Phase:     plan.RepoReady,
			TrackedAt: trackedAt,
		}
		if err := store.UpsertRepo(t.Context(), repo); err != nil {
			t.Fatalf("UpsertRepo(%s): %v", name, err)
		}
	}
}

func createProject(t *testing.T, store *storepkg.Store, p storepkg.NewProject) {
	t.Helper()
	if err := store.CreateProject(t.Context(), p); err != nil {
		t.Fatalf("CreateProject(%s): %v", p.Name, err)
	}
}

func readProjects(t *testing.T, store *storepkg.Store) []storepkg.Project {
	t.Helper()
	projects, err := store.Projects(t.Context())
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	return projects
}

func readRepos(t *testing.T, store *storepkg.Store) []storepkg.Repo {
	t.Helper()
	repos, err := store.Repos(t.Context())
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	return repos
}

func TestStore_CreateProject(t *testing.T) {
	t.Parallel()

	t.Run("reads back with sorted repos", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		trackRepos(t, store, "acme/web", "acme/api", "acme/loose")
		createProject(t, store, storepkg.NewProject{Name: "acme", Hue: 2, Repos: []string{"acme/web", "acme/api"}, At: trackedAt})

		want := []storepkg.Project{{Name: "acme", Hue: 2, Repos: []string{"acme/api", "acme/web"}}}
		if diff := cmp.Diff(want, readProjects(t, store), cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("Projects() mismatch (-want +got):\n%s", diff)
		}
		repos := readRepos(t, store)
		loose := repos[slices.IndexFunc(repos, func(r storepkg.Repo) bool { return r.Name == "acme/loose" })]
		if loose.Project != "" || loose.Hue != 0 {
			t.Errorf("Repos()[acme/loose] = project %q hue %d, want none", loose.Project, loose.Hue)
		}
	})

	t.Run("moves a repo out of its project", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		trackRepos(t, store, "acme/web", "acme/api")
		createProject(t, store, storepkg.NewProject{Name: "old", Hue: 1, Repos: []string{"acme/web", "acme/api"}, At: trackedAt})
		createProject(t, store, storepkg.NewProject{Name: "new", Hue: 3, Repos: []string{"acme/web"}, At: trackedAt})

		want := []storepkg.Project{
			{Name: "new", Hue: 3, Repos: []string{"acme/web"}},
			{Name: "old", Hue: 1, Repos: []string{"acme/api"}},
		}
		if diff := cmp.Diff(want, readProjects(t, store), cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("Projects() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name    string
			project storepkg.NewProject
			want    error
		}{
			{name: "empty name", project: storepkg.NewProject{Name: "", Hue: 1}, want: storepkg.ErrInvalidProject},
			{name: "has a space", project: storepkg.NewProject{Name: "a b", Hue: 1}, want: storepkg.ErrInvalidProject},
			{name: "leading dot", project: storepkg.NewProject{Name: ".acme", Hue: 1}, want: storepkg.ErrInvalidProject},
			{name: "too long", project: storepkg.NewProject{Name: strings.Repeat("a", 41), Hue: 1}, want: storepkg.ErrInvalidProject},
			{name: "hue zero", project: storepkg.NewProject{Name: "acme", Hue: 0}, want: storepkg.ErrInvalidProject},
			{name: "hue four", project: storepkg.NewProject{Name: "acme", Hue: 4}, want: storepkg.ErrInvalidProject},
			{name: "unknown repo", project: storepkg.NewProject{Name: "acme", Hue: 1, Repos: []string{"acme/ghost"}}, want: storepkg.ErrNotFound},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				store := openStore(t)
				tt.project.At = trackedAt
				if err := store.CreateProject(t.Context(), tt.project); !errors.Is(err, tt.want) {
					t.Errorf("CreateProject(%q, %d) error = %v, want %v", tt.project.Name, tt.project.Hue, err, tt.want)
				}
				if got := readProjects(t, store); len(got) != 0 {
					t.Errorf("Projects() after a refused create = %+v, want none", got)
				}
			})
		}
	})

	t.Run("a case-only duplicate is refused", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		trackRepos(t, store, "acme/web", "acme/api")
		createProject(t, store, storepkg.NewProject{Name: "acme", Hue: 1, Repos: []string{"acme/web"}, At: trackedAt})

		err := store.CreateProject(t.Context(), storepkg.NewProject{Name: "ACME", Hue: 1, Repos: []string{"acme/api"}, At: trackedAt})
		if !errors.Is(err, storepkg.ErrProjectExists) {
			t.Fatalf("CreateProject(ACME) error = %v, want ErrProjectExists", err)
		}
		want := []storepkg.Project{{Name: "acme", Hue: 1, Repos: []string{"acme/web"}}}
		if diff := cmp.Diff(want, readProjects(t, store), cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("Projects() mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestStore_SetRepoProject(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) *storepkg.Store {
		t.Helper()
		store := openStore(t)
		trackRepos(t, store, "acme/web")
		createProject(t, store, storepkg.NewProject{Name: "one", Hue: 1, At: trackedAt})
		createProject(t, store, storepkg.NewProject{Name: "two", Hue: 2, At: trackedAt})
		return store
	}

	t.Run("moves a repo case-insensitively, then loosens it", func(t *testing.T) {
		t.Parallel()
		store := setup(t)
		if err := store.SetRepoProject(t.Context(), "acme/web", "TWO"); err != nil {
			t.Fatalf("SetRepoProject(TWO): %v", err)
		}
		if got := readRepos(t, store)[0]; got.Project != "two" || got.Hue != 2 {
			t.Errorf("Repos()[0] = project %q hue %d, want two 2", got.Project, got.Hue)
		}
		if err := store.SetRepoProject(t.Context(), "acme/web", ""); err != nil {
			t.Fatalf("SetRepoProject(empty): %v", err)
		}
		if got := readRepos(t, store)[0].Project; got != "" {
			t.Errorf("Repos()[0].Project = %q, want loose", got)
		}
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name, repo, project string
		}{
			{name: "unknown project", repo: "acme/web", project: "nope"},
			{name: "unknown repo", repo: "acme/ghost", project: "one"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				if err := setup(t).SetRepoProject(t.Context(), tt.repo, tt.project); !errors.Is(err, storepkg.ErrNotFound) {
					t.Errorf("SetRepoProject(%s, %s) error = %v, want ErrNotFound", tt.repo, tt.project, err)
				}
			})
		}
	})
}

func TestStore_EditProject(t *testing.T) {
	t.Parallel()

	t.Run("renames and rehues keeping repos", func(t *testing.T) {
		t.Parallel()
		store := openStore(t)
		trackRepos(t, store, "acme/web", "acme/api")
		createProject(t, store, storepkg.NewProject{Name: "acme", Hue: 1, Repos: []string{"acme/web", "acme/api"}, At: trackedAt})

		if err := store.EditProject(t.Context(), "acme", "acme-co", 1); err != nil {
			t.Fatalf("EditProject rename: %v", err)
		}
		want := []storepkg.Project{{Name: "acme-co", Hue: 1, Repos: []string{"acme/api", "acme/web"}}}
		if diff := cmp.Diff(want, readProjects(t, store), cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("Projects() after rename mismatch (-want +got):\n%s", diff)
		}
		if err := store.EditProject(t.Context(), "acme-co", "acme-co", 3); err != nil {
			t.Fatalf("EditProject hue: %v", err)
		}
		want[0].Hue = 3
		if diff := cmp.Diff(want, readProjects(t, store), cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("Projects() after rehue mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name, from, to string
			hue            int
			want           error
		}{
			{name: "onto an existing name", from: "acme", to: "BETA", hue: 1, want: storepkg.ErrProjectExists},
			{name: "unknown project", from: "ghost", to: "x", hue: 1, want: storepkg.ErrNotFound},
			{name: "hue four", from: "acme", to: "acme", hue: 4, want: storepkg.ErrInvalidProject},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				store := openStore(t)
				createProject(t, store, storepkg.NewProject{Name: "acme", Hue: 1, At: trackedAt})
				createProject(t, store, storepkg.NewProject{Name: "beta", Hue: 1, At: trackedAt})

				if err := store.EditProject(t.Context(), tt.from, tt.to, tt.hue); !errors.Is(err, tt.want) {
					t.Errorf("EditProject(%s, %s, %d) error = %v, want %v", tt.from, tt.to, tt.hue, err, tt.want)
				}
				want := []storepkg.Project{{Name: "acme", Hue: 1}, {Name: "beta", Hue: 1}}
				if diff := cmp.Diff(want, readProjects(t, store), cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("Projects() after a refused edit mismatch (-want +got):\n%s", diff)
				}
			})
		}
	})
}

func TestStore_DeleteProject(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	trackRepos(t, store, "acme/web")
	createProject(t, store, storepkg.NewProject{Name: "acme", Hue: 2, Repos: []string{"acme/web"}, At: trackedAt})

	if err := store.DeleteProject(t.Context(), "acme"); err != nil {
		t.Fatalf("DeleteProject(acme): %v", err)
	}
	if got := readProjects(t, store); len(got) != 0 {
		t.Errorf("Projects() after DeleteProject = %+v, want none", got)
	}
	if repos := readRepos(t, store); len(repos) != 1 || repos[0].Project != "" {
		t.Errorf("Repos() after DeleteProject = %+v, want acme/web tracked and loose", repos)
	}
	if err := store.DeleteProject(t.Context(), "acme"); !errors.Is(err, storepkg.ErrNotFound) {
		t.Errorf("DeleteProject(acme) again error = %v, want ErrNotFound", err)
	}
}
