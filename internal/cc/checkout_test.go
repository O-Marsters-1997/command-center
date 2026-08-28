package cc_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

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
		if !cc.SameRemote(same[0], form) {
			t.Errorf("%q and %q are the same repository, but were read as different", same[0], form)
		}
	}

	for _, other := range []string{
		"git@github.com:o/other.git",
		"git@github.com:other/r.git",
		"git@gitlab.com:o/r.git",
	} {
		if cc.SameRemote(same[0], other) {
			t.Errorf("%q and %q are different repositories, but were read as the same", same[0], other)
		}
	}
}

func TestCheckoutPathRefusesBothFormsOrNeither(t *testing.T) {
	t.Parallel()

	both := "[[repo]]\nname = \"r\"\nremote = \"git@github.com:o/r.git\"\npath = \"r\"\n"
	if _, err := cc.LoadConfig(writeConfig(t, both)); err == nil ||
		!strings.Contains(err.Error(), "r") || !strings.Contains(err.Error(), "both") {
		t.Errorf("error = %v, want one naming the repo that sets both remote and path", err)
	}

	neither := "[[repo]]\nname = \"r\"\n"
	if _, err := cc.LoadConfig(writeConfig(t, neither)); err == nil || !strings.Contains(err.Error(), "neither") {
		t.Errorf("error = %v, want one naming the repo that sets neither", err)
	}
}

func TestLoadConfigPutsARemoteRepoUnderTheDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CC_DATA_DIR", dataDir)

	got, err := cc.LoadConfig(writeConfig(t,
		"[[repo]]\nname = \"command-center\"\nremote = \"git@github.com:o/command-center.git\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dataDir, "repos", "command-center"); got.Repos[0].Checkout != want {
		t.Errorf("checkout = %q, want %q", got.Repos[0].Checkout, want)
	}
}

func TestLoadConfigRefusesARemoteRepoNamedLikeAPath(t *testing.T) {
	t.Setenv("CC_DATA_DIR", t.TempDir())

	_, err := cc.LoadConfig(writeConfig(t,
		"[[repo]]\nname = \"../escape\"\nremote = \"git@github.com:o/r.git\"\n"))
	if err == nil || !strings.Contains(err.Error(), "escape") {
		t.Errorf("error = %v, want one refusing a repo name that is not a single directory", err)
	}
}

func TestEnsureCheckoutClonesThenReusesTheClone(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	remote := filepath.Join(filepath.Dir(repoPath), "remote.git")
	checkout := filepath.Join(t.TempDir(), "repos", "r")

	repo := cc.Repo{Name: "r", Remote: remote, Checkout: checkout}
	if err := cc.EnsureCheckout(t.Context(), repo); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(checkout, "README.md")); err != nil {
		t.Fatalf("clone did not produce a working tree: %v", err)
	}

	marker := filepath.Join(checkout, "not-from-a-second-clone")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cc.EnsureCheckout(t.Context(), repo); err != nil {
		t.Fatalf("second EnsureCheckout: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("the second start re-cloned over the existing checkout")
	}
}

func TestEnsureCheckoutRefusesAMismatchedOrigin(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	realRemote := filepath.Join(filepath.Dir(repoPath), "remote.git")

	const configured = "git@github.com:someone/else.git"
	err := cc.EnsureCheckout(t.Context(), cc.Repo{Name: "r", Remote: configured, Checkout: repoPath})
	if err == nil {
		t.Fatal("want a refusal for a checkout whose origin names a different repository")
	}
	for _, want := range []string{realRemote, configured} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestEnsureCheckoutRefusesADirectoryThatIsNotAGitRepo(t *testing.T) {
	dir := t.TempDir()
	err := cc.EnsureCheckout(t.Context(), cc.Repo{Name: "r", Remote: "git@github.com:o/r.git", Checkout: dir})
	if err == nil || !strings.Contains(err.Error(), dir) {
		t.Errorf("error = %v, want one naming the directory that is not a git repository", err)
	}
}

// TestEnsureCheckoutAcceptsTheOtherURLFormForTheSameRepo is the case that makes remote portable:
// a checkout cloned over ssh, configured over https, is one repository.
func TestEnsureCheckoutAcceptsTheOtherURLFormForTheSameRepo(t *testing.T) {
	_, repoPath := repoWithOrigin(t)
	runGit(t, "-C", repoPath, "remote", "set-url", "origin", "git@github.com:o/r.git")

	// The fetch that follows fails, since git@github.com:o/r is not a repository anyone can
	// reach from a test. What must not happen is the origin comparison refusing first.
	repo := cc.Repo{Name: "r", Remote: "https://github.com/o/r", Checkout: repoPath}
	if err := cc.EnsureCheckout(t.Context(), repo); err != nil && strings.Contains(err.Error(), "the config says") {
		t.Errorf("EnsureCheckout refused the same repository in its other URL form: %v", err)
	}
}
