package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/command"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

const longTimeout = 10 * time.Minute

var ErrOriginMismatch = errors.New("checkout origin differs from the configured remote")

// EnsureCheckout clones a missing checkout or verifies its origin without resetting it.
// An empty remote means a local path repo, never cloned or origin-checked.
func EnsureCheckout(ctx context.Context, name, remote, seedPath, checkout string) error {
	switch _, err := os.Stat(checkout); {
	case errors.Is(err, os.ErrNotExist):
		if remote == "" {
			return fmt.Errorf("repo %s: no checkout at %s, and no remote to clone from", name, checkout)
		}
		return populateCheckout(ctx, name, remote, seedPath, checkout)
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
	if err := configure(ctx, checkout); err != nil {
		return fmt.Errorf("configure %s: %w", checkout, err)
	}
	if err := Fetch(ctx, checkout); err != nil {
		return fmt.Errorf("fetch checkout %s: %w", checkout, err)
	}
	return nil
}

func populateCheckout(ctx context.Context, name, remote, seedPath, checkout string) (err error) {
	tmp := checkout + ".cloning"
	if err := os.RemoveAll(tmp); err != nil {
		return fmt.Errorf("repo %s: remove leftover %s: %w", name, tmp, err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(tmp))
		}
	}()

	populate := func() error { return clone(ctx, name, remote, tmp) }
	if seedPath != "" {
		populate = func() error { return seed(ctx, name, remote, seedPath, tmp) }
	}
	if err := populate(); err != nil {
		return fmt.Errorf("clone repo %s: %w", name, err)
	}
	if err := configure(ctx, tmp); err != nil {
		return fmt.Errorf("configure %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, checkout); err != nil {
		return fmt.Errorf("repo %s: move %s into place: %w", name, tmp, err)
	}
	return nil
}

func configure(ctx context.Context, repoPath string) error {
	for _, key := range []string{"rerere.enabled", "rerere.autoupdate"} {
		if _, err := git(ctx, repoPath, "config", key, "true"); err != nil {
			return fmt.Errorf("enable %s in %s: %w", key, repoPath, err)
		}
	}
	dir, err := packDir(ctx, repoPath)
	if err != nil {
		return err
	}
	if err := sweepTmpPacks(dir); err != nil {
		return err
	}
	return makeFull(ctx, repoPath, dir)
}

func packDir(ctx context.Context, repoPath string) (string, error) {
	out, err := git(ctx, repoPath, "rev-parse", "--path-format=absolute", "--git-path", "objects/pack")
	if err != nil {
		return "", fmt.Errorf("locate pack directory: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func sweepTmpPacks(packDir string) error {
	tmps, err := filepath.Glob(filepath.Join(packDir, "tmp_*"))
	if err != nil {
		return fmt.Errorf("list temp packs: %w", err)
	}
	for _, tmp := range tmps {
		info, err := os.Stat(tmp)
		switch {
		case errors.Is(err, os.ErrNotExist), err == nil && time.Since(info.ModTime()) < longTimeout:
			continue
		case err != nil:
			return fmt.Errorf("stat temp pack %s: %w", tmp, err)
		}
		if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove temp pack %s: %w", tmp, err)
		}
	}
	return nil
}

func makeFull(ctx context.Context, repoPath, packDir string) error {
	_, filtered, err := gitRun(ctx, repoPath, "config", "--get", "remote.origin.partialclonefilter")
	if err != nil {
		return fmt.Errorf("read partial clone filter of %s: %w", repoPath, err)
	}
	promisors, err := filepath.Glob(filepath.Join(packDir, "*.promisor"))
	if err != nil {
		return fmt.Errorf("list promisor packs: %w", err)
	}
	if !filtered && len(promisors) == 0 {
		return nil
	}
	refetch := []string{"fetch", "--refetch", "--no-filter", "origin"}
	if err := command.RunWithin(ctx, longTimeout, repoPath, "git", refetch...); err != nil {
		return fmt.Errorf("refetch %s without a filter: %w", repoPath, err)
	}
	if _, err := git(ctx, repoPath, "config", "remote.origin.promisor", "false"); err != nil {
		return fmt.Errorf("unmark origin as promisor in %s: %w", repoPath, err)
	}
	if err := command.RunWithin(ctx, longTimeout, repoPath, "git", "repack", "-a", "-d", "-q"); err != nil {
		return fmt.Errorf("repack %s: %w", repoPath, err)
	}
	promisors, err = filepath.Glob(filepath.Join(packDir, "*.promisor"))
	if err != nil {
		return fmt.Errorf("list promisor packs: %w", err)
	}
	for _, p := range promisors {
		if err := os.Remove(p); err != nil {
			return fmt.Errorf("remove promisor marker %s: %w", p, err)
		}
	}
	if !filtered {
		return nil
	}
	if _, err := git(ctx, repoPath, "config", "--unset", "remote.origin.partialclonefilter"); err != nil {
		return fmt.Errorf("unset partial clone filter in %s: %w", repoPath, err)
	}
	return nil
}

func seed(ctx context.Context, name, remote, seedPath, checkout string) error {
	origin, err := OriginURL(ctx, seedPath)
	if err != nil {
		return fmt.Errorf("repo %s: seed path %s is not a git repository with an origin: %w", name, seedPath, err)
	}
	if !SameRemote(origin, remote) {
		return fmt.Errorf("repo %s: seed path %s has origin %s, but the tracked remote is %s: %w",
			name, seedPath, origin, remote, ErrOriginMismatch)
	}
	if err := os.MkdirAll(filepath.Dir(checkout), 0o700); err != nil {
		return fmt.Errorf("repo %s: create %s: %w", name, filepath.Dir(checkout), err)
	}
	if err := command.RunWithin(ctx, longTimeout, "", "git", "clone", "--local", seedPath, checkout); err != nil {
		return fmt.Errorf("repo %s: copy %s: %w", name, seedPath, err)
	}
	if _, err := git(ctx, checkout, "remote", "set-url", "origin", remote); err != nil {
		return fmt.Errorf("repo %s: point origin at %s: %w", name, remote, err)
	}
	return nil
}

func clone(ctx context.Context, name, remote, checkout string) error {
	if err := os.MkdirAll(filepath.Dir(checkout), 0o700); err != nil {
		return fmt.Errorf("repo %s: create %s: %w", name, filepath.Dir(checkout), err)
	}
	if err := command.RunWithin(ctx, longTimeout, "", "git", "clone", remote, checkout); err != nil {
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
