package ghtest_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/adapter/gh/ghtest"
)

func TestForge(t *testing.T) {
	t.Parallel()
	ghtest.RunContract(t, func(_ *testing.T, seed ghtest.Seed) gh.Forge {
		byBranch := make(map[string]gh.PR, len(seed.PRs))
		for _, pr := range seed.PRs {
			byBranch[pr.HeadRef] = pr
		}
		return &ghtest.Forge{Snapshot: gh.Snapshot{ByBranch: byBranch}, Titles: seed.Titles}
	})
}
