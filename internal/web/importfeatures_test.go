package web

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

type featureSource struct{ features []tracker.Feature }

func (f featureSource) Features(context.Context) ([]tracker.Feature, error) { return f.features, nil }

func (featureSource) Tickets(context.Context, string) ([]tracker.Ticket, error) {
	return nil, fmt.Errorf("listing features must not read tickets")
}

func TestImportFeaturesListsLabelsAcrossRepos(t *testing.T) {
	t.Parallel()

	repos := []config.Repo{
		{Name: "alpha", Remote: "git@github.com:acme/alpha.git"},
		{Name: "beta", Remote: "git@github.com:acme/beta.git"},
		{Name: "local", Path: "."},
	}
	byRemote := map[string]tracker.Source{
		"github.com/acme/alpha": featureSource{[]tracker.Feature{"project:x"}},
		"github.com/acme/beta":  featureSource{[]tracker.Feature{"project:x", "project:y"}},
	}
	resolve := func(_ tracker.Kind, remote string) (tracker.Source, error) {
		src, ok := byRemote[remote]
		if !ok {
			return nil, fmt.Errorf("no source for %q", remote)
		}
		return src, nil
	}

	got, err := importFeatures(t.Context(), repos, nil, resolve)
	if err != nil {
		t.Fatalf("importFeatures: %v", err)
	}
	if want := []string{"project:x", "project:y"}; !slices.Equal(got, want) {
		t.Errorf("features = %v, want %v", got, want)
	}
}

func TestImportFeaturesDispatchesOnEachReposConfiguredTrackerKind(t *testing.T) {
	t.Parallel()

	repos := []config.Repo{
		{Name: "alpha", Remote: "git@github.com:acme/alpha.git"},
		{Name: "beta", Remote: "git@github.com:acme/beta.git"},
	}
	settings := map[string]config.RepoSettings{"alpha": {Tracker: "github"}, "beta": {Tracker: "linear"}}
	var gotKinds []tracker.Kind
	resolve := func(kind tracker.Kind, _ string) (tracker.Source, error) {
		gotKinds = append(gotKinds, kind)
		return featureSource{}, nil
	}

	if _, err := importFeatures(t.Context(), repos, settings, resolve); err != nil {
		t.Fatalf("importFeatures: %v", err)
	}
	if want := []tracker.Kind{"github", "linear"}; !slices.Equal(gotKinds, want) {
		t.Errorf("kinds passed to resolve = %v, want %v", gotKinds, want)
	}
}
