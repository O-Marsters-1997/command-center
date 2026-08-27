package cc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CheckoutPath answers where a repo's working copy is. A remote repo's checkout is one the app
// makes and names, at <dataDir>/repos/<name>; a path repo's is one the operator made, absolute
// or relative to configDir. Exactly one of the two forms is allowed.
func (r Repo) CheckoutPath(dataDir, configDir string) (string, error) {
	switch {
	case r.Remote != "" && r.Path != "":
		return "", fmt.Errorf("repo %s sets both remote and path: pick one", r.Name)
	case r.Remote == "" && r.Path == "":
		return "", fmt.Errorf("repo %s sets neither remote nor path", r.Name)
	case r.Remote != "":
		if err := validRepoName(r.Name); err != nil {
			return "", err
		}
		return filepath.Join(dataDir, "repos", r.Name), nil
	}

	path, err := expandHome(r.Path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(configDir, path)
	}
	return filepath.Clean(path), nil
}

func validRepoName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("repo name %q is not a single directory name", name)
	}
	return nil
}

// EnsureCheckout clones repo when its checkout directory is absent, and otherwise verifies that
// what is there has the configured origin. It never resets, pulls or checks anything out: work
// happens in the worktrees tp cuts beside the checkout, not in it.
func EnsureCheckout(ctx context.Context, repo Repo) error {
	switch _, err := os.Stat(repo.Checkout); {
	case errors.Is(err, os.ErrNotExist):
		if repo.Remote == "" {
			return fmt.Errorf("repo %s: no checkout at %s, and no remote to clone from",
				repo.Name, repo.Checkout)
		}
		return clone(ctx, repo)
	case err != nil:
		return fmt.Errorf("repo %s: stat %s: %w", repo.Name, repo.Checkout, err)
	}

	origin, err := originURL(ctx, repo.Checkout)
	if err != nil {
		return fmt.Errorf("repo %s: %s is not a git repository with an origin: %w",
			repo.Name, repo.Checkout, err)
	}
	if repo.Remote != "" && !sameRemote(origin, repo.Remote) {
		return fmt.Errorf("repo %s: %s has origin %s, but the config says %s",
			repo.Name, repo.Checkout, origin, repo.Remote)
	}
	return Fetch(ctx, repo.Checkout)
}

func clone(ctx context.Context, repo Repo) error {
	if err := os.MkdirAll(filepath.Dir(repo.Checkout), 0o700); err != nil {
		return fmt.Errorf("repo %s: create %s: %w", repo.Name, filepath.Dir(repo.Checkout), err)
	}
	cmd := exec.CommandContext(ctx, "git", "clone", repo.Remote, repo.Checkout)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("clone %s into %s: %w: %s", repo.Remote, repo.Checkout, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func originURL(ctx context.Context, repoPath string) (string, error) {
	out, err := git(ctx, repoPath, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// sameRemote decides whether two git URLs name one repository. The ssh and https forms differ in
// scheme, in carrying a user, and in the .git suffix, so all three are stripped before comparing.
func sameRemote(a, b string) bool {
	return normaliseRemote(a) == normaliseRemote(b)
}

func normaliseRemote(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")

	if scheme := strings.Index(url, "://"); scheme >= 0 {
		url = url[scheme+3:]
	} else if colon := strings.Index(url, ":"); colon >= 0 && !strings.Contains(url[:colon], "/") {
		// scp-like: git@github.com:owner/repo
		url = url[:colon] + "/" + url[colon+1:]
	}
	if at := strings.Index(url, "@"); at >= 0 {
		url = url[at+1:]
	}
	return strings.ToLower(strings.TrimSuffix(url, "/"))
}
