package register

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

// SandboxCheckout is the e2e build's cc.CheckoutFunc. A path-based repo gets the real
// EnsureCheckout; a remote-based one exists only so import.go's repo-matching has a real
// host+owner+repo string to check tickets against, and its checkout is a symlink, never a clone.
func SandboxCheckout(ctx context.Context, repos []cc.Repo) error {
	for _, repo := range repos {
		if repo.Path != "" {
			if err := cc.EnsureCheckout(ctx, repo); err != nil {
				return err
			}
			continue
		}
		if err := ensureSandboxSymlink(ctx, repo); err != nil {
			return err
		}
	}
	return nil
}

// ensureSandboxSymlink links repo.Checkout to the sandbox repo instead of cloning it. `git
// rev-parse --show-toplevel` resolves a symlinked cwd to its real target, so tp's worktree
// siblings and every $WORK/<repo> assertion see the same path a real checkout would.
func ensureSandboxSymlink(ctx context.Context, repo cc.Repo) error {
	_, err := os.Lstat(repo.Checkout)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("repo %s: stat %s: %w", repo.Name, repo.Checkout, err)
	}

	work := os.Getenv("CC_WORK_DIR")
	if work == "" {
		return fmt.Errorf("repo %s: CC_WORK_DIR is not set", repo.Name)
	}
	src := filepath.Join(work, repo.Name)
	if err := os.MkdirAll(filepath.Dir(repo.Checkout), 0o700); err != nil {
		return fmt.Errorf("repo %s: create %s: %w", repo.Name, filepath.Dir(repo.Checkout), err)
	}
	if err := os.Symlink(src, repo.Checkout); err != nil {
		return fmt.Errorf("repo %s: symlink %s to %s: %w", repo.Name, repo.Checkout, src, err)
	}

	for _, key := range []string{"rerere.enabled", "rerere.autoupdate"} {
		cmd := exec.CommandContext(ctx, "git", "-C", repo.Checkout, "config", key, "true")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("repo %s: git config %s: %w: %s", repo.Name, key, err, out)
		}
	}
	return nil
}
