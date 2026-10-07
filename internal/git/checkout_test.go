package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type repoConfig struct{ Name, Remote, Checkout string }

func runGit(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func runGitOutput(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

func repoWithOrigin(t *testing.T) (root, repoPath string) {
	t.Helper()
	root = t.TempDir()
	repoPath = filepath.Join(root, "repo")
	remote := filepath.Join(root, "remote.git")

	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	runGit(t, "init", "-q", "-b", "main", repoPath)
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, "-C", repoPath, "add", "README.md")
	runGit(t, "-C", repoPath, "commit", "-q", "-m", "initial")
	runGit(t, "init", "-q", "-b", "main", "--bare", remote)
	runGit(t, "-C", repoPath, "remote", "add", "origin", remote)
	runGit(t, "-C", repoPath, "push", "-q", "-u", "origin", "main")
	runGit(t, "-C", repoPath, "fetch", "-q", "origin")
	return root, repoPath
}

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
		if !SameRemote(same[0], form) {
			t.Errorf("%q and %q are the same repository, but were read as different", same[0], form)
		}
	}

	for _, other := range []string{
		"git@github.com:o/other.git",
		"git@github.com:other/r.git",
		"git@gitlab.com:o/r.git",
	} {
		if SameRemote(same[0], other) {
			t.Errorf("%q and %q are different repositories, but were read as the same", same[0], other)
		}
	}
}

func TestEnsureCheckoutClonesThenReusesTheClone(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	remote := filepath.Join(filepath.Dir(repoPath), "remote.git")
	checkout := filepath.Join(t.TempDir(), "repos", "r")

	repo := repoConfig{Name: "r", Remote: remote, Checkout: checkout}
	if err := EnsureCheckout(t.Context(), repo.Name, repo.Remote, repo.Checkout); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(checkout, "README.md")); err != nil {
		t.Fatalf("clone did not produce a working tree: %v", err)
	}

	marker := filepath.Join(checkout, "not-from-a-second-clone")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCheckout(t.Context(), repo.Name, repo.Remote, repo.Checkout); err != nil {
		t.Fatalf("second EnsureCheckout: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("the second start re-cloned over the existing checkout")
	}
}

func TestEnsureCheckoutRefuses(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	realRemote := filepath.Join(filepath.Dir(repoPath), "remote.git")
	notARepo := t.TempDir()
	const configured = "git@github.com:someone/else.git"

	tests := []struct {
		name     string
		remote   string
		checkout string
		wantInfo []string
	}{
		{"a mismatched origin", configured, repoPath, []string{realRemote, configured}},
		{"a directory that is not a git repo", "git@github.com:o/r.git", notARepo, []string{notARepo}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := EnsureCheckout(t.Context(), "r", tt.remote, tt.checkout)
			if err == nil {
				t.Fatal("want a refusal")
			}
			for _, want := range tt.wantInfo {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}

// TestEnsureCheckoutAcceptsTheOtherURLFormForTheSameRepo is the case that makes remote portable:
// a checkout cloned over ssh, configured over https, is one repository.
func TestEnsureCheckoutAcceptsTheOtherURLFormForTheSameRepo(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	runGit(t, "-C", repoPath, "remote", "set-url", "origin", "git@github.com:o/r.git")

	// The fetch that follows fails, since git@github.com:o/r is not a repository anyone can
	// reach from a test. What must not happen is the origin comparison refusing first.
	repo := repoConfig{Name: "r", Remote: "https://github.com/o/r", Checkout: repoPath}
	if err := EnsureCheckout(t.Context(), repo.Name, repo.Remote, repo.Checkout); err != nil &&
		strings.Contains(err.Error(), "the config says") {
		t.Errorf("EnsureCheckout refused the same repository in its other URL form: %v", err)
	}
}

// TestEnsureCheckoutEnablesRerere covers both arms. rerere is local config, so a re-clone on a
// fresh machine would otherwise lose it, and the live checkout predates the change.
func TestEnsureCheckoutEnablesRerere(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	remote := filepath.Join(filepath.Dir(repoPath), "remote.git")
	checkout := filepath.Join(t.TempDir(), "repos", "r")
	repo := repoConfig{Name: "r", Remote: remote, Checkout: checkout}

	if err := EnsureCheckout(t.Context(), repo.Name, repo.Remote, repo.Checkout); err != nil {
		t.Fatalf("clone: %v", err)
	}
	assertRerereOn(t, checkout)

	runGit(t, "-C", checkout, "config", "--unset", "rerere.enabled")
	runGit(t, "-C", checkout, "config", "--unset", "rerere.autoupdate")
	if err := EnsureCheckout(t.Context(), repo.Name, repo.Remote, repo.Checkout); err != nil {
		t.Fatalf("second EnsureCheckout: %v", err)
	}
	assertRerereOn(t, checkout)

	if err := EnsureCheckout(t.Context(), repo.Name, repo.Remote, repo.Checkout); err != nil {
		t.Fatalf("third EnsureCheckout: %v", err)
	}
	assertRerereOn(t, checkout)
}

func assertRerereOn(t *testing.T, checkout string) {
	t.Helper()
	for _, key := range []string{"rerere.enabled", "rerere.autoupdate"} {
		got := strings.TrimSpace(runGitOutput(t, "-C", checkout, "config", "--get", key))
		if got != "true" {
			t.Errorf("%s = %q, want \"true\"", key, got)
		}
	}
}
