//go:build unix

package metertest_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/meter"
	"github.com/O-Marsters-1997/command-center/internal/adapter/meter/metertest"
)

func TestMeter(t *testing.T) {
	t.Parallel()
	metertest.RunContract(t, func(_ *testing.T, world []meter.Process) meter.Meter {
		return metertest.Meter{Total: 8 << 30, Used: 4 << 30, Procs: world}
	})
}
