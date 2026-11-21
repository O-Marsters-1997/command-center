package ghtest_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/adapter/gh/ghtest"
)

func TestForge(t *testing.T) {
	t.Parallel()
	ghtest.RunContract(t, func(_ *testing.T, seed ghtest.Seed) gh.Forge {
		forge := &ghtest.Forge{}
		forge.Seed(seed)
		return forge
	})
}
