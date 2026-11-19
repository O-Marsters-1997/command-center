package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

func TestSameRemote(t *testing.T) {
	t.Parallel()

	const canonical = "git@github.com:o/r.git"
	tests := []struct {
		other string
		want  bool
	}{
		{other: "git@github.com:o/r", want: true},
		{other: "https://github.com/o/r", want: true},
		{other: "https://github.com/o/r.git", want: true},
		{other: "https://github.com/o/r/", want: true},
		{other: "ssh://git@github.com/o/r.git", want: true},
		{other: "git@github.com:o/other.git", want: false},
		{other: "git@github.com:other/r.git", want: false},
		{other: "git@gitlab.com:o/r.git", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.other, func(t *testing.T) {
			t.Parallel()
			if got := git.SameRemote(canonical, tt.other); got != tt.want {
				t.Errorf("SameRemote(%q, %q) = %v, want %v", canonical, tt.other, got, tt.want)
			}
		})
	}
}

func rerere(t *testing.T, checkout string) []string {
	t.Helper()
	return []string{
		gittest.Run(t, checkout, "config", "--get", "rerere.enabled"),
		gittest.Run(t, checkout, "config", "--get", "rerere.autoupdate"),
	}
}

func partialSeed(t *testing.T) (seedPath, remote string) {
	t.Helper()
	repo, origin := gittest.WithOrigin(t)
	gittest.Commit(t, repo, "README.md", "second\n")
	gittest.Run(t, repo, "push", "-q", "origin", "main")
	gittest.Run(t, origin, "config", "uploadpack.allowFilter", "true")
	remote = "file://" + origin
	seedPath = filepath.Join(t.TempDir(), "seed")
	gittest.Run(t, ".", "clone", "-q", "--filter=blob:none", remote, seedPath)
	return seedPath, remote
}

func TestEnsureCheckout(t *testing.T) {
	t.Parallel()

	t.Run("clones, then reuses the clone", func(t *testing.T) {
		t.Parallel()
		_, remote := gittest.WithOrigin(t)
		checkout := filepath.Join(t.TempDir(), "repos", "r")
		if err := git.EnsureCheckout(t.Context(), "r", remote, "", checkout); err != nil {
			t.Fatalf("EnsureCheckout(clone) error = %v", err)
		}
		marker := filepath.Join(checkout, "not-from-a-second-clone")
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			t.Fatalf("WriteFile(%s): %v", marker, err)
		}

		if err := git.EnsureCheckout(t.Context(), "r", remote, "", checkout); err != nil {
			t.Fatalf("EnsureCheckout(reuse) error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(checkout, "README.md")); err != nil {
			t.Errorf("EnsureCheckout(clone) left no working tree: %v", err)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("EnsureCheckout(reuse) re-cloned over the existing checkout: %v", err)
		}
	})

	t.Run("enables rerere on clone and on every reuse", func(t *testing.T) {
		t.Parallel()
		_, remote := gittest.WithOrigin(t)
		checkout := filepath.Join(t.TempDir(), "repos", "r")
		if err := git.EnsureCheckout(t.Context(), "r", remote, "", checkout); err != nil {
			t.Fatalf("EnsureCheckout(clone) error = %v", err)
		}
		if got := rerere(t, checkout); got[0] != "true" || got[1] != "true" {
			t.Errorf("EnsureCheckout(clone) rerere.enabled, rerere.autoupdate = %v, want true, true", got)
		}
		gittest.Run(t, checkout, "config", "--unset", "rerere.enabled")
		gittest.Run(t, checkout, "config", "--unset", "rerere.autoupdate")

		if err := git.EnsureCheckout(t.Context(), "r", remote, "", checkout); err != nil {
			t.Fatalf("EnsureCheckout(reuse) error = %v", err)
		}
		if got := rerere(t, checkout); got[0] != "true" || got[1] != "true" {
			t.Errorf("EnsureCheckout(reuse) rerere.enabled, rerere.autoupdate = %v, want true, true", got)
		}
	})

	t.Run("accepts the other URL form of the same repo", func(t *testing.T) {
		t.Parallel()
		repoPath, _ := gittest.WithOrigin(t)
		gittest.Run(t, repoPath, "config", "core.sshCommand", "false")
		gittest.Run(t, repoPath, "remote", "set-url", "origin", "git@github.com:o/r.git")

		err := git.EnsureCheckout(t.Context(), "r", "https://github.com/o/r", "", repoPath)
		if errors.Is(err, git.ErrOriginMismatch) {
			t.Errorf("EnsureCheckout(https form of origin) error = %v, want no ErrOriginMismatch", err)
		}
	})

	t.Run("refuses a mismatched origin", func(t *testing.T) {
		t.Parallel()
		repoPath, _ := gittest.WithOrigin(t)
		const configured = "git@github.com:someone/else.git"
		if err := git.EnsureCheckout(t.Context(), "r", configured, "", repoPath); !errors.Is(err, git.ErrOriginMismatch) {
			t.Errorf("EnsureCheckout(%q) error = %v, want ErrOriginMismatch", configured, err)
		}
	})

	t.Run("refuses a directory that is not a git repo", func(t *testing.T) {
		t.Parallel()
		err := git.EnsureCheckout(t.Context(), "r", "git@github.com:o/r.git", "", t.TempDir())
		if err == nil || errors.Is(err, git.ErrOriginMismatch) {
			t.Errorf("EnsureCheckout(non-git dir) error = %v, want an error other than ErrOriginMismatch", err)
		}
	})

	t.Run("seeds by copy with origin on the tracked remote", func(t *testing.T) {
		t.Parallel()
		seedPath, remote := gittest.WithOrigin(t)
		checkout := filepath.Join(t.TempDir(), "repos", "r")
		if err := git.EnsureCheckout(t.Context(), "r", remote, seedPath, checkout); err != nil {
			t.Fatalf("EnsureCheckout(seed) error = %v", err)
		}
		if got, err := git.OriginURL(t.Context(), checkout); err != nil || got != remote {
			t.Errorf("OriginURL(seeded) = %q, %v, want %q", got, err, remote)
		}
		if err := git.EnsureCheckout(t.Context(), "r", remote, "", checkout); err != nil {
			t.Errorf("EnsureCheckout(first fetch after seed) error = %v", err)
		}
	})

	t.Run("seeded from a partial clone, the checkout can repack, and reuse repairs one that cannot", func(t *testing.T) {
		t.Parallel()
		seedPath, remote := partialSeed(t)
		checkout := filepath.Join(t.TempDir(), "repos", "r")
		if err := git.EnsureCheckout(t.Context(), "r", remote, seedPath, checkout); err != nil {
			t.Fatalf("EnsureCheckout(seed) error = %v", err)
		}
		gittest.Run(t, checkout, "repack", "-a", "-d", "-q")

		gittest.Run(t, checkout, "config", "--unset", "remote.origin.promisor")
		if err := git.EnsureCheckout(t.Context(), "r", remote, "", checkout); err != nil {
			t.Fatalf("EnsureCheckout(reuse) error = %v", err)
		}
		gittest.Run(t, checkout, "repack", "-a", "-d", "-q")
	})

	t.Run("replaces a leftover temporary clone", func(t *testing.T) {
		t.Parallel()
		_, remote := gittest.WithOrigin(t)
		checkout := filepath.Join(t.TempDir(), "repos", "r")
		leftover := checkout + ".cloning"
		if err := os.MkdirAll(filepath.Join(leftover, "half"), 0o700); err != nil {
			t.Fatalf("MkdirAll(%s): %v", leftover, err)
		}
		if err := git.EnsureCheckout(t.Context(), "r", remote, "", checkout); err != nil {
			t.Fatalf("EnsureCheckout error = %v", err)
		}
		if _, err := os.Stat(filepath.Join(checkout, "README.md")); err != nil {
			t.Errorf("EnsureCheckout left no working tree: %v", err)
		}
		if _, err := os.Stat(leftover); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("EnsureCheckout left %s behind: %v", leftover, err)
		}
	})

	t.Run("a failed clone leaves nothing behind", func(t *testing.T) {
		t.Parallel()
		checkout := filepath.Join(t.TempDir(), "repos", "r")
		if err := git.EnsureCheckout(t.Context(), "r", "file://"+t.TempDir(), "", checkout); err == nil {
			t.Fatal("EnsureCheckout(unclonable remote) error = nil, want error")
		}
		for _, p := range []string{checkout, checkout + ".cloning"} {
			if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("EnsureCheckout left %s behind: %v", p, err)
			}
		}
	})

	t.Run("refuses a seed that is not a repo or has another origin, leaving no directory", func(t *testing.T) {
		t.Parallel()
		otherRepo, _ := gittest.WithOrigin(t)
		tests := []struct {
			name     string
			seedPath string
			wantIs   error
		}{
			{name: "not a git repo", seedPath: t.TempDir()},
			{name: "origin differs", seedPath: otherRepo, wantIs: git.ErrOriginMismatch},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				checkout := filepath.Join(t.TempDir(), "repos", "r")
				err := git.EnsureCheckout(t.Context(), "r", "git@github.com:o/r.git", tt.seedPath, checkout)
				if err == nil || (tt.wantIs != nil && !errors.Is(err, tt.wantIs)) {
					t.Errorf("EnsureCheckout(seed %s) error = %v, want error matching %v", tt.name, err, tt.wantIs)
				}
				if _, statErr := os.Stat(checkout); !errors.Is(statErr, os.ErrNotExist) {
					t.Errorf("EnsureCheckout(seed %s) left %s behind: %v", tt.name, checkout, statErr)
				}
			})
		}
	})
}

func TestFullName(t *testing.T) {
	t.Parallel()

	for _, remote := range []string{
		"git@github.com:O-Marsters-1997/command-center.git",
		"https://github.com/O-Marsters-1997/command-center",
		"ssh://git@github.com/O-Marsters-1997/command-center.git/",
	} {
		t.Run(remote, func(t *testing.T) {
			t.Parallel()
			got, err := git.FullName(remote)
			if err != nil || got != "O-Marsters-1997/command-center" {
				t.Errorf("FullName(%q) = %q, %v, want O-Marsters-1997/command-center, nil", remote, got, err)
			}
		})
	}

	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		for _, remote := range []string{
			"", "/tmp/remote.git", "https://github.com/o", "https://host/a/b/c", "git@github.com:../evil.git",
			"https://github.com/o/..", "githuB.Com:0/.git.git", "git@github.com:owner/repo.git.git",
			"https://github.com/owner/repo.git.git",
		} {
			if got, err := git.FullName(remote); err == nil {
				t.Errorf("FullName(%q) = %q, want an error", remote, got)
			}
		}
	})
}

var githubFullName = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

func FuzzFullName(f *testing.F) {
	f.Fuzz(func(t *testing.T, remote string) {
		fullName, err := git.FullName(remote)
		if err != nil {
			return
		}
		normal := plan.NormaliseRemote(remote)
		if !strings.HasPrefix(normal, "github.com/") ||
			!githubFullName.MatchString(strings.TrimPrefix(normal, "github.com/")) {
			return
		}
		if !git.SameRemote(remote, "https://github.com/"+fullName) {
			t.Errorf("FullName(%q) = %q, which does not name the same GitHub remote", remote, fullName)
		}
	})
}
