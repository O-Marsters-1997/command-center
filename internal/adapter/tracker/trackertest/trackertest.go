package trackertest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/O-Marsters-1997/command-center/internal/adapter/tracker"
)

type Source struct {
	ByFeature map[string][]tracker.Ticket

	mu          sync.Mutex
	ticketReads int
}

var _ tracker.Source = (*Source)(nil)

func (s *Source) Features(context.Context) ([]tracker.Feature, error) {
	features := []tracker.Feature{}
	for _, name := range slices.Sorted(maps.Keys(s.ByFeature)) {
		features = append(features, tracker.Feature(name))
	}
	return features, nil
}

func (s *Source) Tickets(_ context.Context, feature string) ([]tracker.Ticket, error) {
	s.mu.Lock()
	s.ticketReads++
	s.mu.Unlock()
	return slices.Clone(s.ByFeature[feature]), nil
}

func (s *Source) TicketReads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ticketReads
}

func ByRemote(sources map[string]tracker.Source) tracker.Resolver {
	return func(_ tracker.Kind, remote string) (tracker.Source, error) {
		src, ok := sources[remote]
		if !ok {
			return nil, fmt.Errorf("trackertest: no source for %q", remote)
		}
		return src, nil
	}
}

func RunContract(t *testing.T, newSource func(t *testing.T, byFeature map[string][]tracker.Ticket) tracker.Source) {
	t.Helper()
	byFeature := map[string][]tracker.Ticket{
		"project:x": {
			{URL: "https://github.com/o/r/issues/1", Number: 1, Title: "One", Body: "b", Status: "ready", BlockedBy: []string{}},
			{
				URL: "https://github.com/o/r/issues/2", Number: 2, Title: "Two", Status: "backlog",
				BlockedBy: []string{"https://github.com/o/r/issues/1"},
			},
		},
		"project:y": {{URL: "https://github.com/o/r/issues/3", Number: 3, Title: "Three", BlockedBy: []string{}}},
	}

	t.Run("Features lists every feature", func(t *testing.T) {
		t.Parallel()
		got, err := newSource(t, byFeature).Features(t.Context())
		if err != nil {
			t.Fatalf("Features() error = %v", err)
		}
		want := []tracker.Feature{"project:x", "project:y"}
		if diff := cmp.Diff(want, got, cmpopts.SortSlices(func(a, b tracker.Feature) bool { return a < b })); diff != "" {
			t.Errorf("Features() mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Tickets returns a feature's tickets", func(t *testing.T) {
		t.Parallel()
		got, err := newSource(t, byFeature).Tickets(t.Context(), "project:x")
		if err != nil {
			t.Fatalf("Tickets(project:x) error = %v", err)
		}
		if diff := cmp.Diff(byFeature["project:x"], got, cmpopts.EquateEmpty()); diff != "" {
			t.Errorf("Tickets(project:x) mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("Tickets of an unknown feature is empty", func(t *testing.T) {
		t.Parallel()
		got, err := newSource(t, byFeature).Tickets(t.Context(), "project:absent")
		if err != nil {
			t.Fatalf("Tickets(project:absent) error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("Tickets(project:absent) = %+v, want none", got)
		}
	})
}
