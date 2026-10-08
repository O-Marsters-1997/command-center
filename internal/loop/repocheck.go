package loop

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/git"
)

// AssertReposSquashOnly checks every configured repo's merge settings, once each, before the
// tick loop starts.
func AssertReposSquashOnly(ctx context.Context, repos []config.Repo) error {
	for _, r := range repos {
		if err := git.CheckSquashOnly(ctx, r.Checkout, r.Name); err != nil {
			return err
		}
	}
	return nil
}
