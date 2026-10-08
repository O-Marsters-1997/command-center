package web

import (
	"context"
	"fmt"
	"sort"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

func importFeatures(ctx context.Context, repos []config.Repo, resolve tracker.Resolver) ([]string, error) {
	var names []string
	seen := map[string]bool{}
	for _, r := range repos {
		src, ok, err := tracker.ForRemote(resolve, tracker.Kind(r.Tracker), r.Remote)
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
			if !seen[string(f)] {
				seen[string(f)] = true
				names = append(names, string(f))
			}
		}
	}
	sort.Strings(names)
	return names, nil
}
