package trackertest_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/tracker/trackertest"
)

func TestByRemoteResolvesFeaturesAndTickets(t *testing.T) {
	t.Parallel()
	source := &trackertest.Source{
		Features: []tracker.Feature{"project:x"},
		Tickets:  map[string][]tracker.Ticket{"project:x": {{URL: "https://example.com/1"}}},
	}
	resolve := trackertest.ByRemote(map[string]*trackertest.Source{"github.com/acme/repo": source})
	got, err := resolve(tracker.GitHub, "github.com/acme/repo")
	if err != nil {
		t.Fatalf("resolve known remote: %v", err)
	}
	features, err := got.Features(t.Context())
	if err != nil {
		t.Fatalf("Features: %v", err)
	}
	if diff := cmp.Diff(source.Features, features); diff != "" {
		t.Errorf("Features (-want +got):\n%s", diff)
	}
	tickets, err := got.Tickets(t.Context(), "project:x")
	if err != nil {
		t.Fatalf("Tickets: %v", err)
	}
	if diff := cmp.Diff(source.Tickets["project:x"], tickets); diff != "" {
		t.Errorf("Tickets (-want +got):\n%s", diff)
	}
	missing, err := got.Tickets(t.Context(), "project:missing")
	if err != nil || len(missing) != 0 {
		t.Errorf("Tickets(missing) = %v, %v, want empty", missing, err)
	}
	if _, err := resolve(tracker.GitHub, "github.com/acme/other"); err == nil {
		t.Error("resolve unknown remote = nil error, want not found")
	}
}
