package trackertest_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/tracker"
	"github.com/O-Marsters-1997/command-center/internal/tracker/trackertest"
)

func TestSource(t *testing.T) {
	t.Parallel()
	trackertest.RunContract(t, func(_ *testing.T, byFeature map[string][]tracker.Ticket) tracker.Source {
		return &trackertest.Source{ByFeature: byFeature}
	})
}
