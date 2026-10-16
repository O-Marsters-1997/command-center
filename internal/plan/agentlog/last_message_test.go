package agentlog_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
)

func TestParseLastMessage(t *testing.T) {
	t.Parallel()

	t.Run("a settled run yields its final result text", func(t *testing.T) {
		t.Parallel()
		got, err := agentlog.ParseLastMessage(filepath.Join("testdata", "run27.jsonl"))
		if err != nil {
			t.Fatalf("ParseLastMessage error = %v", err)
		}
		if !got.IsSome() || !strings.HasPrefix(got.Unwrap(), "Committed. Working tree is clean.") {
			t.Errorf("ParseLastMessage() = %v, want the final result text", got)
		}
	})

	t.Run("a result line without text yields none", func(t *testing.T) {
		t.Parallel()
		got, err := agentlog.ParseLastMessage(filepath.Join("testdata", "alive.jsonl"))
		if err != nil {
			t.Fatalf("ParseLastMessage error = %v", err)
		}
		if got.IsSome() {
			t.Errorf("ParseLastMessage() = %v, want none", got)
		}
	})

	t.Run("an absent log is an error", func(t *testing.T) {
		t.Parallel()
		if _, err := agentlog.ParseLastMessage(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
			t.Error("ParseLastMessage error = nil, want one")
		}
	})
}
