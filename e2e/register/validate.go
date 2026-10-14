package register

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/command"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// SandboxValidate is the e2e build's loop.ValidateFunc. The harness's cc-config symlinks each
// tracked repo's checkout to its sandbox, because a tracked remote is undialable; this only
// enables rerere on it, as git.EnsureCheckout would on a real clone, and settles the repo ready.
func SandboxValidate(ctx context.Context, dataDir string, repo store.Repo, now time.Time) (store.Repo, error) {
	checkout := config.CheckoutPath(dataDir, repo.Name)
	for _, key := range []string{"rerere.enabled", "rerere.autoupdate"} {
		if err := command.Run(ctx, checkout, "git", "config", key, "true"); err != nil {
			return repo, fmt.Errorf("repo %s: git config %s: %w", repo.Name, key, err)
		}
	}
	repo.Phase, repo.RefusalKind, repo.Refusal = plan.RepoReady, plan.RefusalUnknown, ""
	repo.SettingsSource, repo.SettingsReadAt = "defaults", now
	return repo, nil
}

// SandboxRemote is the e2e build's loop.RemoteFunc: a fixed SSH URL, so Track needs no GitHub.
func SandboxRemote(_ context.Context, fullName string) (string, error) {
	return "git@github.com:" + fullName + ".git", nil
}
