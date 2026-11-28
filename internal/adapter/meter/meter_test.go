//go:build unix

package meter_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/meter"
	"github.com/O-Marsters-1997/command-center/internal/adapter/meter/metertest"
)

func TestPS(t *testing.T) {
	t.Parallel()
	metertest.RunContract(t, func(*testing.T, []meter.Process) meter.Meter { return meter.PS{} })
}
