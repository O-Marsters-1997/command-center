package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
)

type repoConfig struct{ Name, Remote, Checkout string }

func TestSameRemoteAcrossURLForms(t *testing.T) {
	t.Parallel()

	same := []string{
		"git@github.com:o/r.git",
		"git@github.com:o/r",
		"https://github.com/o/r",
		"https://github.com/o/r.git",
		"https://github.com/o/r/",
		"ssh://git@github.com/o/r.git",
	}
	for _, form := range same[1:] {
		if !git.SameRemote(same[0], form) {
			t.Errorf("%q and %q are the same repository, but were read as different", same[0], form)
		}
	}

	for _, other := range []string{
		"git@github.com:o/other.git",
		"git@github.com:other/r.git",
		"git@gitlab.com:o/r.git",
	} {
		if git.SameRemote(same[0], other) {
			t.Errorf("%q and %q are different repositories, but were read as the same", same[0], other)
		}
	}
}

func TestEnsureCheckoutClonesThenReusesTheClone(t *testing.T) {
	t.Parallel()
	_, remote := gittest.WithOrigin(t)
	checkout := filepath.Join(t.TempDir(), "repos", "r")

	repo := repoConfig{Name: "r", Remote: remote, Checkout: checkout}
	if err := git.EnsureCheckout(t.Context(), repo.Name, repo.Remote, repo.Checkout); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(checkout, "README.md")); err != nil {
		t.Fatalf("clone did not produce a working tree: %v", err)
	}

	marker := filepath.Join(checkout, "not-from-a-second-clone")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", marker, err)
	}
	if err := git.EnsureCheckout(t.Context(), repo.Name, repo.Remote, repo.Checkout); err != nil {
		t.Fatalf("second EnsureCheckout: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("the second start re-cloned over the existing checkout")
	}
}

func TestEnsureCheckoutRefusesAMismatchedOrigin(t *testing.T) {
	t.Parallel()
	repoPath, realRemote := gittest.WithOrigin(t)
	const configured = "git@github.com:someone/else.git"
	if err := git.EnsureCheckout(t.Context(), "r", configured, repoPath); !errors.Is(err, git.ErrOriginMismatch) {
		t.Errorf("EnsureCheckout(%q, %q) = %v, want ErrOriginMismatch (actual remote %q)",
			configured, repoPath, err, realRemote)
	}
}

func TestEnsureCheckoutRefusesANonGitDirectory(t *testing.T) {
	t.Parallel()
	err := git.EnsureCheckout(t.Context(), "r", "git@github.com:o/r.git", t.TempDir())
	if err == nil || errors.Is(err, git.ErrOriginMismatch) {
		t.Errorf("EnsureCheckout(non-git dir) = %v, want an error other than ErrOriginMismatch", err)
	}
}

func TestEnsureCheckoutAcceptsTheOtherURLFormForTheSameRepo(t *testing.T) {
	t.Parallel()
	repoPath, _ := gittest.WithOrigin(t)
	gittest.Run(t, repoPath, "config", "core.sshCommand", "false")
	gittest.Run(t, repoPath, "remote", "set-url", "origin", "git@github.com:o/r.git")

	repo := repoConfig{Name: "r", Remote: "https://github.com/o/r", Checkout: repoPath}
	err := git.EnsureCheckout(t.Context(), repo.Name, repo.Remote, repo.Checkout)
	if errors.Is(err, git.ErrOriginMismatch) {
		t.Errorf("EnsureCheckout refused the same repository in its other URL form: %v", err)
	}
}

func TestEnsureCheckoutEnablesRerere(t *testing.T) {
	t.Parallel()
	_, remote := gittest.WithOrigin(t)
	checkout := filepath.Join(t.TempDir(), "repos", "r")
	repo := repoConfig{Name: "r", Remote: remote, Checkout: checkout}

	if err := git.EnsureCheckout(t.Context(), repo.Name, repo.Remote, repo.Checkout); err != nil {
		t.Fatalf("clone: %v", err)
	}
	assertRerereOn(t, checkout)

	gittest.Run(t, checkout, "config", "--unset", "rerere.enabled")
	gittest.Run(t, checkout, "config", "--unset", "rerere.autoupdate")
	if err := git.EnsureCheckout(t.Context(), repo.Name, repo.Remote, repo.Checkout); err != nil {
		t.Fatalf("second EnsureCheckout: %v", err)
	}
	assertRerereOn(t, checkout)

	if err := git.EnsureCheckout(t.Context(), repo.Name, repo.Remote, repo.Checkout); err != nil {
		t.Fatalf("third EnsureCheckout: %v", err)
	}
	assertRerereOn(t, checkout)
}

func assertRerereOn(t *testing.T, checkout string) {
	t.Helper()
	for _, key := range []string{"rerere.enabled", "rerere.autoupdate"} {
		got := gittest.Run(t, checkout, "config", "--get", key)
		if got != "true" {
			t.Errorf("%s = %q, want \"true\"", key, got)
		}
	}
}

func TestFullNameKeepsCaseAcrossURLForms(t *testing.T) {
	t.Parallel()

	for _, remote := range []string{
		"git@github.com:O-Marsters-1997/command-center.git",
		"https://github.com/O-Marsters-1997/command-center",
		"ssh://git@github.com/O-Marsters-1997/command-center.git/",
	} {
		got, err := git.FullName(remote)
		if err != nil || got != "O-Marsters-1997/command-center" {
			t.Errorf("FullName(%q) = %q, %v, want O-Marsters-1997/command-center", remote, got, err)
		}
	}
	for _, remote := range []string{
		"", "/tmp/remote.git", "https://github.com/o", "https://host/a/b/c", "git@github.com:../evil.git",
		"https://github.com/o/..",
	} {
		if got, err := git.FullName(remote); err == nil {
			t.Errorf("FullName(%q) = %q, want an error", remote, got)
		}
	}
}

func TestFullNameRejectsNonRoundTrippingGitSuffix(t *testing.T) {
	t.Parallel()
	for _, remote := range []string{
		"githuB.Com:0/.git.git",
		"git@github.com:owner/repo.git.git",
		"https://github.com/owner/repo.git.git",
	} {
		if got, err := git.FullName(remote); err == nil {
			t.Errorf("FullName(%q) = %q, want an error", remote, got)
		}
	}
}
