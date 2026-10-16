package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
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

func TestEnsureCheckout(t *testing.T) {
	t.Parallel()

	t.Run("clones, then reuses the clone", func(t *testing.T) {
		t.Parallel()
		_, remote := gittest.WithOrigin(t)
		checkout := filepath.Join(t.TempDir(), "repos", "r")
		if err := git.EnsureCheckout(t.Context(), "r", remote, checkout); err != nil {
			t.Fatalf("EnsureCheckout(clone) error = %v", err)
		}
		marker := filepath.Join(checkout, "not-from-a-second-clone")
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			t.Fatalf("WriteFile(%s): %v", marker, err)
		}

		if err := git.EnsureCheckout(t.Context(), "r", remote, checkout); err != nil {
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
		if err := git.EnsureCheckout(t.Context(), "r", remote, checkout); err != nil {
			t.Fatalf("EnsureCheckout(clone) error = %v", err)
		}
		if got := rerere(t, checkout); got[0] != "true" || got[1] != "true" {
			t.Errorf("EnsureCheckout(clone) rerere.enabled, rerere.autoupdate = %v, want true, true", got)
		}
		gittest.Run(t, checkout, "config", "--unset", "rerere.enabled")
		gittest.Run(t, checkout, "config", "--unset", "rerere.autoupdate")

		if err := git.EnsureCheckout(t.Context(), "r", remote, checkout); err != nil {
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

		err := git.EnsureCheckout(t.Context(), "r", "https://github.com/o/r", repoPath)
		if errors.Is(err, git.ErrOriginMismatch) {
			t.Errorf("EnsureCheckout(https form of origin) error = %v, want no ErrOriginMismatch", err)
		}
	})

	t.Run("refuses a mismatched origin", func(t *testing.T) {
		t.Parallel()
		repoPath, _ := gittest.WithOrigin(t)
		const configured = "git@github.com:someone/else.git"
		if err := git.EnsureCheckout(t.Context(), "r", configured, repoPath); !errors.Is(err, git.ErrOriginMismatch) {
			t.Errorf("EnsureCheckout(%q) error = %v, want ErrOriginMismatch", configured, err)
		}
	})

	t.Run("refuses a directory that is not a git repo", func(t *testing.T) {
		t.Parallel()
		err := git.EnsureCheckout(t.Context(), "r", "git@github.com:o/r.git", t.TempDir())
		if err == nil || errors.Is(err, git.ErrOriginMismatch) {
			t.Errorf("EnsureCheckout(non-git dir) error = %v, want an error other than ErrOriginMismatch", err)
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
