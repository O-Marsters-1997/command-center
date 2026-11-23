package agentlog_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/plan/spend"
)

func oneMillionInputTokensLine(timestamp, requestID string) string {
	return fmt.Sprintf(
		`{"type":"assistant","timestamp":%q,"request_id":%q,`+
			`"message":{"model":"claude-sonnet-5","usage":{"input_tokens":1000000}}}`,
		timestamp, requestID,
	)
}

func TestLoadRequests(t *testing.T) {
	t.Parallel()

	t.Run("counts an interactive transcript and its subagent", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		project := filepath.Join(dir, "-Users-olly-some-project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatalf("MkdirAll(project): %v", err)
		}
		files := map[string]string{
			"session.jsonl": oneMillionInputTokensLine("2026-01-01T00:00:00.000Z", "r1") + "\n" +
				oneMillionInputTokensLine("2026-01-01T01:00:00.000Z", "r2") + "\n",
			"subagent-s1.jsonl": oneMillionInputTokensLine("2026-01-01T00:30:00.000Z", "s1") + "\n",
			"notes.txt":         "not a transcript",
		}
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(project, name), []byte(body), 0o644); err != nil {
				t.Fatalf("WriteFile(%s): %v", name, err)
			}
		}

		requests, err := agentlog.LoadRequests(dir)
		if err != nil {
			t.Fatalf("LoadRequests() error = %v", err)
		}
		if len(requests) != 3 {
			t.Errorf("LoadRequests(dir) = %d requests, want 3", len(requests))
		}
	})

	t.Run("decodes a Claude Code transcript's requestId", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		body, err := os.ReadFile("testdata/claude_transcript.jsonl")
		if err != nil {
			t.Fatalf("ReadFile(claude_transcript.jsonl): %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), body, 0o644); err != nil {
			t.Fatalf("WriteFile(session.jsonl): %v", err)
		}

		requests, err := agentlog.LoadRequests(dir)
		if err != nil {
			t.Fatalf("LoadRequests() error = %v", err)
		}
		if len(requests) != 2 {
			t.Fatalf("LoadRequests(dir) = %d requests, want 2", len(requests))
		}
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if got := spend.SumWeight(requests, start, start.Add(time.Hour)); got <= 0 {
			t.Errorf("SumWeight(requests) = %v, want above zero", got)
		}
	})

	t.Run("a missing dir is empty", func(t *testing.T) {
		t.Parallel()
		requests, err := agentlog.LoadRequests(filepath.Join(t.TempDir(), "nothing"))
		if err != nil || len(requests) != 0 {
			t.Errorf("LoadRequests(missing) = %d requests, %v, want 0, nil", len(requests), err)
		}
	})
}
