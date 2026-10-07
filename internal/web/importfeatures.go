package web

import (
	"context"
	"fmt"
	"sort"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// importFeatures reads every configured repo's tracker features, gathered fresh on every render
// (inv. 14): one gh label list per repo, and no per-feature ticket call.
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
