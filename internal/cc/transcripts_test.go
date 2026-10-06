package cc

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/spend"
)

func oneMillionInputTokensLine(timestamp, requestID string) string {
	return fmt.Sprintf(
		`{"type":"assistant","timestamp":%q,"request_id":%q,`+
			`"message":{"model":"claude-sonnet-5","usage":{"input_tokens":1000000}}}`,
		timestamp, requestID,
	)
}

func TestLoadRequestsCountsAnInteractiveTranscriptAndItsSubagent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	project := filepath.Join(dir, "-Users-olly-some-project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	interactive := oneMillionInputTokensLine("2026-01-01T00:00:00.000Z", "r1") + "\n" +
		oneMillionInputTokensLine("2026-01-01T01:00:00.000Z", "r2") + "\n"
	if err := os.WriteFile(filepath.Join(project, "session.jsonl"), []byte(interactive), 0o644); err != nil {
		t.Fatal(err)
	}
	subagent := oneMillionInputTokensLine("2026-01-01T00:30:00.000Z", "s1") + "\n"
	if err := os.WriteFile(filepath.Join(project, "subagent-s1.jsonl"), []byte(subagent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "notes.txt"), []byte("not a transcript"), 0o644); err != nil {
		t.Fatal(err)
	}

	requests, err := LoadRequests(dir)
	if err != nil {
		t.Fatalf("LoadRequests: %v", err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)

	// r1 and s1 are in range; r2 lands exactly at end, which is exclusive.
	const sonnetInputPerMillion = 6.40
	got, want := spend.SumWeight(requests, start, end), 2*sonnetInputPerMillion
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("SumWeight(LoadRequests(dir)) = %v, want %v", got, want)
	}
}

func TestLoadRequestsOnAMissingDirIsEmpty(t *testing.T) {
	t.Parallel()

	requests, err := LoadRequests(filepath.Join(t.TempDir(), "nothing"))
	if err != nil {
		t.Fatalf("LoadRequests: %v", err)
	}
	if len(requests) != 0 {
		t.Errorf("LoadRequests on a missing dir = %d requests, want 0", len(requests))
	}
}
