package cc

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

// importVerb is the intent verb the loop's applyImportIntents consumes. It is not in
// supportedVerbs (verbs.go): that map is for verbs against an existing ticket, and an import
// intent's own "ticket" id is a feature label, not a url.
const importVerb = "import"

// QueueImport queues one import intent for feature, which applyImportIntents performs on the
// loop's next tick.
func QueueImport(ctx context.Context, store *store.Store, feature string, at time.Time) error {
	return store.QueueVerbIntent(ctx, feature, importVerb, at)
}

// TrackerSource resolves the tracker.Source that reads one repo's tracker. tracker.New in
// production; a test substitutes a fake here rather than shelling out to gh.
type TrackerSource func(kind tracker.Kind, remote string) (tracker.Source, error)

// ImportFeature is one project: label a configured repo's tracker offers.
type ImportFeature struct {
	Feature string
}

// ImportFeatures reads every configured repo's tracker features, gathered fresh on every render
// (inv. 14): one gh label list per repo, and no per-feature ticket call. A repo with no remote
// has no tracker to dispatch to and is silently skipped.
func ImportFeatures(ctx context.Context, repos []config.Repo, resolve TrackerSource) ([]ImportFeature, error) {
	var names []string
	seen := map[string]bool{}
	for _, r := range repos {
		src, ok, err := trackerSourceFor(r, resolve)
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

	result := make([]ImportFeature, 0, len(names))
	for _, name := range names {
		result = append(result, ImportFeature{Feature: name})
	}
	return result, nil
}

func trackerSourceFor(r config.Repo, resolve TrackerSource) (tracker.Source, bool, error) {
	if r.Remote == "" {
		return nil, false, nil
	}
	src, err := resolve(tracker.Kind(r.Tracker), git.NormaliseRemote(r.Remote))
	if err != nil {
		return nil, false, err
	}
	return src, true, nil
}
