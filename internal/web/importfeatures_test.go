package web

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker/trackertest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

func TestImportOffers(t *testing.T) {
	t.Parallel()

	t.Run("sizes an unimported feature and skips reading an imported one", func(t *testing.T) {
		t.Parallel()
		repos := []store.Repo{
			{Name: "acme/alpha", Remote: "git@github.com:acme/alpha.git"},
			{Name: "acme/beta", Remote: "git@github.com:acme/beta.git"},
			{Name: "local"},
		}
		issue := func(n int) tracker.Ticket { return tracker.Ticket{URL: "u", Number: n} }
		alpha := &trackertest.Source{ByFeature: map[string][]tracker.Ticket{
			"project:x": {issue(1)}, "project:y": {issue(2), issue(3)},
		}}
		beta := &trackertest.Source{ByFeature: map[string][]tracker.Ticket{"project:x": {issue(4)}}}
		resolve := trackertest.ByRemote(map[string]tracker.Source{
			"github.com/acme/alpha": alpha,
			"github.com/acme/beta":  beta,
		})
		imported := []store.Ticket{{Repo: "acme/alpha", Feature: "project:x"}}

		got, err := importOffers(t.Context(), repos, nil, resolve, imported)
		if err != nil {
			t.Fatalf("importOffers() error = %v", err)
		}
		want := map[string][]view.ImportOffer{
			"acme/alpha": {{Feature: "project:x", Imported: true}, {Feature: "project:y", Tickets: 2}},
			"acme/beta":  {{Feature: "project:x", Tickets: 1}},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("importOffers() mismatch (-want +got):\n%s", diff)
		}
		if reads := alpha.TicketReads(); reads != 1 {
			t.Errorf("importOffers() read alpha tickets %d times, want 1", reads)
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

		if _, err := importOffers(t.Context(), repos, settings, resolve, nil); err != nil {
			t.Fatalf("importOffers() error = %v", err)
		}
		if diff := cmp.Diff([]tracker.Kind{"github", "linear"}, gotKinds); diff != "" {
			t.Errorf("importOffers() resolved kinds mismatch (-want +got):\n%s", diff)
		}
	})
}
