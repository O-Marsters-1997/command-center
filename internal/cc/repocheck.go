package cc

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/git"
)

// AssertReposSquashOnly checks every configured repo's merge settings, once each, before the
// tick loop starts. It mirrors OpenStore's schema_version check: a precondition the design
// depends on, checked loudly rather than assumed (docs/designs/command-centre-design.md §11.6).
func AssertReposSquashOnly(ctx context.Context, ws Workspace, repos []Repo) error {
	for _, r := range repos {
		if err := git.CheckSquashOnly(ctx, r.Checkout, r.Name); err != nil {
			return err
		}
	}
	return nil
}
