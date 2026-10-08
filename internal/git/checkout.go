package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/command"
)

// EnsureCheckout clones remote into checkout when absent, otherwise verifies its origin. It never
// resets, pulls or checks anything out. An empty remote means a path repo, never cloned or
// origin-checked.
func EnsureCheckout(ctx context.Context, name, remote, checkout string) error {
	switch _, err := os.Stat(checkout); {
	case errors.Is(err, os.ErrNotExist):
		if remote == "" {
			return fmt.Errorf("repo %s: no checkout at %s, and no remote to clone from", name, checkout)
		}
		if err := clone(ctx, name, remote, checkout); err != nil {
			return err
		}
		return enableRerere(ctx, checkout)
	case err != nil:
		return fmt.Errorf("repo %s: stat %s: %w", name, checkout, err)
	}

	origin, err := OriginURL(ctx, checkout)
	if err != nil {
		return fmt.Errorf("repo %s: %s is not a git repository with an origin: %w", name, checkout, err)
	}
	if remote != "" && !SameRemote(origin, remote) {
		return fmt.Errorf("repo %s: %s has origin %s, but the config says %s", name, checkout, origin, remote)
	}
	if err := enableRerere(ctx, checkout); err != nil {
		return err
	}
	return Fetch(ctx, checkout)
}

// A rebase drops the merge commit a resolution was committed as, so every restack re-hits the
// same conflict unless rerere replays it.
func enableRerere(ctx context.Context, repoPath string) error {
	for _, key := range []string{"rerere.enabled", "rerere.autoupdate"} {
		if _, err := git(ctx, repoPath, "config", key, "true"); err != nil {
			return fmt.Errorf("enable %s in %s: %w", key, repoPath, err)
		}
	}
	return nil
}

func clone(ctx context.Context, name, remote, checkout string) error {
	if err := os.MkdirAll(filepath.Dir(checkout), 0o700); err != nil {
		return fmt.Errorf("repo %s: create %s: %w", name, filepath.Dir(checkout), err)
	}
	return command.Run(ctx, "", "git", "clone", remote, checkout)
}

// OriginURL reads repoPath's origin remote URL.
func OriginURL(ctx context.Context, repoPath string) (string, error) {
	out, err := git(ctx, repoPath, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// SameRemote decides whether two git URLs name one repository. The ssh and https forms differ in
// scheme, in carrying a user, and in the .git suffix, so all three are stripped before comparing.
func SameRemote(a, b string) bool {
	return NormaliseRemote(a) == NormaliseRemote(b)
}

// NormaliseRemote reduces a git URL to its lower-case host/owner/repo form.
func NormaliseRemote(url string) string {
	return strings.ToLower(hostPath(url))
}

// FullName reads a remote's owner/name, keeping the case it is written in.
func FullName(remote string) (string, error) {
	host, fullName, _ := strings.Cut(hostPath(remote), "/")
	owner, name, ok := strings.Cut(fullName, "/")
	if !ok || host == "" || !pathSegment(owner) || !pathSegment(name) || strings.Contains(name, "/") {
		return "", fmt.Errorf("remote %q does not name an owner/name repository", remote)
	}
	return fullName, nil
}

func pathSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.Contains(s, `\`)
}

func hostPath(url string) string {
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
	return strings.TrimSuffix(url, "/")
}
