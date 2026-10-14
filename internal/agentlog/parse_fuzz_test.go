package agentlog_test

import (
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

func FuzzParseLine(f *testing.F) {
	f.Fuzz(func(t *testing.T, line []byte) {
		_ = agentlog.ParseLine(line)
	})
}
