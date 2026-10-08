package register

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// SandboxCheckout is the e2e build's app.CheckoutFunc. The harness's cc-config symlinks each
// tracked repo's checkout to its sandbox, because a tracked remote is undialable; this only
// enables rerere on it, as git.EnsureCheckout would on a real clone.
func SandboxCheckout(ctx context.Context, dataDir string, repos []store.Repo) error {
	for _, repo := range repos {
		checkout := config.CheckoutPath(dataDir, repo.Name)
		for _, key := range []string{"rerere.enabled", "rerere.autoupdate"} {
			cmd := exec.CommandContext(ctx, "git", "-C", checkout, "config", key, "true")
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("repo %s: git config %s: %w: %s", repo.Name, key, err, out)
			}
		}
	}
	return nil
}
