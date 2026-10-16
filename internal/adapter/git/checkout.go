package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/adapter/command"
	"github.com/O-Marsters-1997/command-center/internal/adapter/tp"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

var ErrOriginMismatch = errors.New("checkout origin differs from the configured remote")

// EnsureCheckout clones a missing checkout or verifies its origin without resetting it.
// An empty remote means a local path repo, never cloned or origin-checked.
func EnsureCheckout(ctx context.Context, name, remote, checkout string) error {
	switch _, err := os.Stat(checkout); {
	case errors.Is(err, os.ErrNotExist):
		if remote == "" {
			return fmt.Errorf("repo %s: no checkout at %s, and no remote to clone from", name, checkout)
		}
		if err := clone(ctx, name, remote, checkout); err != nil {
			return fmt.Errorf("clone repo %s: %w", name, err)
		}
		if err := enableRerere(ctx, checkout); err != nil {
			return fmt.Errorf("configure rerere for %s: %w", checkout, err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("repo %s: stat %s: %w", name, checkout, err)
	}

	origin, err := OriginURL(ctx, checkout)
	if err != nil {
		return fmt.Errorf("repo %s: %s is not a git repository with an origin: %w", name, checkout, err)
	}
	if remote != "" && !SameRemote(origin, remote) {
		return fmt.Errorf("repo %s: %s has origin %s, but the config says %s: %w",
			name, checkout, origin, remote, ErrOriginMismatch)
	}
	if err := enableRerere(ctx, checkout); err != nil {
		return fmt.Errorf("configure rerere for %s: %w", checkout, err)
	}
	if err := Fetch(ctx, checkout); err != nil {
		return fmt.Errorf("fetch checkout %s: %w", checkout, err)
	}
	return nil
}

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
	if err := command.RunWithin(ctx, tp.LongTimeout, "", "git", "clone", remote, checkout); err != nil {
		return fmt.Errorf("repo %s: clone %s: %w", name, remote, err)
	}
	return nil
}

func OriginURL(ctx context.Context, repoPath string) (string, error) {
	out, err := git(ctx, repoPath, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("read origin URL: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func SameRemote(a, b string) bool {
	return plan.NormaliseRemote(a) == plan.NormaliseRemote(b)
}

func FullName(remote string) (string, error) {
	host, fullName, _ := strings.Cut(hostPath(remote), "/")
	owner, name, ok := strings.Cut(fullName, "/")
	if !ok || host == "" || !pathSegment(owner) || !pathSegment(name) ||
		strings.Contains(name, "/") || strings.HasSuffix(name, ".git") {
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
		url = url[:colon] + "/" + url[colon+1:]
	}
	if at := strings.Index(url, "@"); at >= 0 {
		url = url[at+1:]
	}
	return strings.TrimSuffix(url, "/")
}
