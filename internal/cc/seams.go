package cc

import (
	"os"
	"path/filepath"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// composePrompt resolves a task's seams against root/.claude/seams/<name>, in config order. A
// seam with no readable file, missing or otherwise, is a refusal, not an empty paste
// (docs/designs/command-centre-design.md § 6): refusedSeam names it and ok is false.
func composePrompt(root string, t plan.Task) (composed, refusedSeam string, ok bool) {
	contents := make([]string, 0, len(t.Seams))
	for _, name := range t.Seams {
		data, err := os.ReadFile(filepath.Join(root, ".claude", "seams", name))
		if err != nil {
			return "", name, false
		}
		contents = append(contents, string(data))
	}
	return plan.Compose(t, contents), "", true
}
