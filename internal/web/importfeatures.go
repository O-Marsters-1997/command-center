package web

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

func importFeatures(
	ctx context.Context, repos []store.Repo, settings map[string]config.RepoSettings, resolve tracker.Resolver,
) ([]string, error) {
	seen := map[string]struct{}{}
	for _, r := range repos {
		kind := config.DefaultRepoSettings().Tracker
		if s, ok := settings[r.Name]; ok {
			kind = s.Tracker
		}
		src, ok, err := tracker.ForRemote(resolve, tracker.Kind(kind), r.Remote)
		if err != nil {
			return nil, fmt.Errorf("repo %s: %w", r.Name, err)
		}
		if !ok {
			continue
		}

		features, err := src.Features(ctx)
		if err != nil {
			return nil, fmt.Errorf("list features for %s: %w", r.Name, err)
		}
		for _, f := range features {
			seen[string(f)] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(seen)), nil
}
