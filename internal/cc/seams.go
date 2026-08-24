package cc

import (
	"context"
	"os"
	"path/filepath"

	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// retirement is a seam's resolved lands_at source for one tick: repoPath's origin/main.
type retirement struct {
	repoPath string
	landsAt  []string
}

func retirementsByName(
	seams []Seam, byURL map[string]plan.Task, prs map[string]plan.PRState, repoPaths map[string]string,
) map[string]retirement {
	out := make(map[string]retirement, len(seams))
	for _, s := range seams {
		if len(s.LandsAt) == 0 || !allProducersMerged(s.Producers, byURL, prs) {
			continue
		}
		out[s.Name] = retirement{repoPath: repoPaths[s.Repo], landsAt: s.LandsAt}
	}
	return out
}

func allProducersMerged(producers []string, byURL map[string]plan.Task, prs map[string]plan.PRState) bool {
	if len(producers) == 0 {
		return false
	}
	for _, ticketURL := range producers {
		t, ok := byURL[ticketURL]
		if !ok || prs[t.Branch] != plan.Merged {
			return false
		}
	}
	return true
}

// composePrompt resolves a task's seams, preferring a retirement over the seam file. A seam or
// lands_at path with no readable content is a refusal, not an empty paste: refused names
// whichever one, and ok is false.
func composePrompt(
	ctx context.Context, root string, t plan.Task, retirements map[string]retirement,
) (composed, refused string, ok bool) {
	contents := make([]string, 0, len(t.Seams))
	for _, name := range t.Seams {
		if r, retired := retirements[name]; retired {
			for _, path := range r.landsAt {
				data, err := ShowFile(ctx, r.repoPath, "origin/main", path)
				if err != nil {
					return "", path, false
				}
				contents = append(contents, data)
			}
			continue
		}

		data, err := os.ReadFile(filepath.Join(root, ".claude", "seams", name))
		if err != nil {
			return "", name, false
		}
		contents = append(contents, string(data))
	}
	return plan.Compose(t, contents), "", true
}
