//go:build unix

package meter_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/adapter/meter"
	"github.com/O-Marsters-1997/command-center/internal/adapter/meter/metertest"
)

func TestPS(t *testing.T) {
	t.Parallel()
	metertest.RunContract(t, func(*testing.T, []meter.Process) meter.Meter { return meter.PS{} })
}

func TestParsePS(t *testing.T) {
	t.Parallel()
	out := []byte("  100   101  2048 /usr/bin/claude --print\n  100   102   512 gopls serve\n  200   201   64 sleep 30\n")
	got, err := meter.ParsePS(out, []int{100})
	if err != nil {
		t.Fatalf("ParsePS error = %v", err)
	}
	want := []meter.Process{
		{Pgid: 100, Pid: 101, Name: "claude", Args: "/usr/bin/claude --print", RSS: 2048 * 1024},
		{Pgid: 100, Pid: 102, Name: "gopls", Args: "gopls serve", RSS: 512 * 1024},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ParsePS mismatch (-want +got):\n%s", diff)
	}
	if _, err := meter.ParsePS([]byte("100 x 5 cmd\n"), []int{100}); err == nil {
		t.Error("ParsePS(non-numeric pid) error = nil, want error")
	}
}

func TestParseVMStat(t *testing.T) {
	t.Parallel()
	out := []byte("Mach Virtual Memory Statistics: (page size of 16384 bytes)\n" +
		"Pages free:                               10.\n" +
		"Pages active:                            100.\n" +
		"Pages wired down:                         50.\n" +
		"Pages occupied by compressor:             25.\n")
	got, err := meter.ParseVMStat(out)
	if err != nil || got != 175*16384 {
		t.Errorf("ParseVMStat = %d, %v, want %d, nil", got, err, 175*16384)
	}
	if _, err := meter.ParseVMStat([]byte("Pages active: 1.\n")); err == nil {
		t.Error("ParseVMStat(missing counters) error = nil, want error")
	}
}

func TestParseMeminfo(t *testing.T) {
	t.Parallel()
	total, used, err := meter.ParseMeminfo([]byte("MemTotal:       1000 kB\nMemFree: 5 kB\nMemAvailable:    400 kB\n"))
	if err != nil || total != 1000*1024 || used != 600*1024 {
		t.Errorf("ParseMeminfo = %d, %d, %v, want %d, %d, nil", total, used, err, 1000*1024, 600*1024)
	}
	if _, _, err := meter.ParseMeminfo([]byte("MemTotal: 1 kB\n")); err == nil {
		t.Error("ParseMeminfo(no MemAvailable) error = nil, want error")
	}
}
