package tptest_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/tp"
	"github.com/O-Marsters-1997/command-center/internal/tp/tptest"
)

func TestWorktrees(t *testing.T) {
	t.Parallel()
	tptest.RunContract(t, func(*testing.T) tp.Worktrees { return &tptest.Worktrees{} })
}
