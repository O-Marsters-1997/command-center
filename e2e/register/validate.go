package register

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/command"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// SandboxValidate settles a sandbox checkout without dialing its synthetic remote.
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

// SandboxRemote supplies a synthetic SSH origin without contacting GitHub.
func SandboxRemote(_ context.Context, fullName string) (string, error) {
	return "git@github.com:" + fullName + ".git", nil
}
