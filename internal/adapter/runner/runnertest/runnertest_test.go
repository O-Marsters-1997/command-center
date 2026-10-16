package runnertest_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner/runnertest"
)

func TestRunner(t *testing.T) {
	t.Parallel()
	runnertest.RunContract(t, func(*testing.T) runner.Runner { return &runnertest.Runner{} })
}
