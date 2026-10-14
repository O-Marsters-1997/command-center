package tracker_test

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/tracker"
)

func FuzzBranchSlug(f *testing.F) {
	f.Fuzz(func(t *testing.T, number int, title string) {
		slug := tracker.BranchSlug(number, title)
		if !strings.HasPrefix(slug, "cc-"+strconv.Itoa(number)) {
			t.Fatalf("BranchSlug(%d, %q) = %q, want cc-%d prefix", number, title, slug, number)
		}
		if out, err := exec.Command("git", "check-ref-format", "--branch", slug).CombinedOutput(); err != nil {
			t.Errorf("BranchSlug(%d, %q) = %q, invalid branch: %v: %s", number, title, slug, err, out)
		}
	})
}
