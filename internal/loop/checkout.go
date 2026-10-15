package loop

import (
	"context"

	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func RepoNameForDir(ctx context.Context, dir string, repos []store.Repo) (name string, ok bool) {
	origin, err := git.OriginURL(ctx, dir)
	if err != nil {
		return "", false
	}
	for _, r := range repos {
		if git.SameRemote(origin, r.Remote) {
			return r.Name, true
		}
	}
	return "", false
}
