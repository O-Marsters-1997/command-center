package web

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/tracker/trackertest"
)

func TestImportFeatures(t *testing.T) {
	t.Parallel()

	t.Run("lists labels across repos without reading tickets", func(t *testing.T) {
		t.Parallel()
		repos := []store.Repo{
			{Name: "alpha", Remote: "git@github.com:acme/alpha.git"},
			{Name: "beta", Remote: "git@github.com:acme/beta.git"},
			{Name: "local"},
		}
		alpha := &trackertest.Source{ByFeature: map[string][]tracker.Ticket{"project:x": nil}}
		beta := &trackertest.Source{ByFeature: map[string][]tracker.Ticket{"project:x": nil, "project:y": nil}}
		resolve := trackertest.ByRemote(map[string]tracker.Source{
			"github.com/acme/alpha": alpha,
			"github.com/acme/beta":  beta,
		})

		got, err := importFeatures(t.Context(), repos, nil, resolve)
		if err != nil {
			t.Fatalf("importFeatures() error = %v", err)
		}
		if diff := cmp.Diff([]string{"project:x", "project:y"}, got); diff != "" {
			t.Errorf("importFeatures() mismatch (-want +got):\n%s", diff)
		}
		if reads := alpha.TicketReads() + beta.TicketReads(); reads != 0 {
			t.Errorf("importFeatures() read tickets %d times, want 0", reads)
		}
	})

	t.Run("dispatches on each repo's configured tracker kind", func(t *testing.T) {
		t.Parallel()
		repos := []store.Repo{
			{Name: "alpha", Remote: "git@github.com:acme/alpha.git"},
			{Name: "beta", Remote: "git@github.com:acme/beta.git"},
		}
		settings := map[string]config.RepoSettings{"alpha": {Tracker: "github"}, "beta": {Tracker: "linear"}}
		var gotKinds []tracker.Kind
		resolve := func(kind tracker.Kind, _ string) (tracker.Source, error) {
			gotKinds = append(gotKinds, kind)
			return &trackertest.Source{}, nil
		}

		if _, err := importFeatures(t.Context(), repos, settings, resolve); err != nil {
			t.Fatalf("importFeatures() error = %v", err)
		}
		if diff := cmp.Diff([]tracker.Kind{"github", "linear"}, gotKinds); diff != "" {
			t.Errorf("importFeatures() resolved kinds mismatch (-want +got):\n%s", diff)
		}
	})
}
