package loop

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/git"
)

// RepoNameForDir answers which configured repo dir belongs to, using dir's git origin normalised
// and compared the same way git.EnsureCheckout compares an existing checkout's origin. No origin, or
// no configured repo matching it, answers ok=false rather than an error.
func RepoNameForDir(ctx context.Context, dir string, repos []config.Repo) (name string, ok bool) {
	origin, err := git.OriginURL(ctx, dir)
	if err != nil {
		return "", false
	}
	for _, r := range repos {
		if r.Remote != "" && git.SameRemote(origin, r.Remote) {
			return r.Name, true
		}
	}
	return "", false
}
