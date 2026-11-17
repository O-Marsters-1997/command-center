package loop_test

import (
	"os"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
)

func TestMain(m *testing.M) { os.Exit(gittest.RunMain(m)) }
