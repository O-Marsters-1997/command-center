package loop

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// AssertReposSquashOnly checks every tracked repo's merge settings, once each, before the
// tick loop starts.
func AssertReposSquashOnly(ctx context.Context, dataDir string, repos []store.Repo) error {
	for _, r := range repos {
		if err := git.CheckSquashOnly(ctx, config.CheckoutPath(dataDir, r.Name), r.Name); err != nil {
			return err
		}
	}
	return nil
}
