package usage_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/usage"
)

// oneMillionInputTokensLine is one $6.40 (at the calibrated sonnet rate) assistant request, for a
// test to place at a chosen timestamp and request id.
func oneMillionInputTokensLine(timestamp, requestID string) string {
	return fmt.Sprintf(
		`{"type":"assistant","timestamp":%q,"request_id":%q,`+
			`"message":{"model":"claude-sonnet-5","usage":{"input_tokens":1000000}}}`,
		timestamp, requestID,
	)
}

// TestWeighCountsAnInteractiveTranscriptAndItsSubagent covers CC-313's acceptance criterion: an
// interval that includes interactive transcripts counts their weight -- alongside a subagent
// transcript sitting as an ordinary sibling file, and excludes a request outside the span and a
// non-transcript file.
func TestWeighCountsAnInteractiveTranscriptAndItsSubagent(t *testing.T) {
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

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)

	got, err := usage.Weigh(dir, start, end)
	if err != nil {
		t.Fatalf("Weigh: %v", err)
	}

	// Only r1 (at start, in-range) and s1 (the subagent, in-range) count; r2 lands exactly at end,
	// which is exclusive, and notes.txt is not a transcript at all.
	const sonnetInputPerMillion = 6.40
	want := 2 * sonnetInputPerMillion
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Weigh = %v; want %v", got, want)
	}
}

// TestSumWeightPrefersASettledRequestsOwnExactCost covers a settled run's own reported
// total_cost_usd taking priority over agentlog.Weight's token-price estimate, which the token
// counts here would put nowhere near.
func TestSumWeightPrefersASettledRequestsOwnExactCost(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	exact := 123.45
	settled := agentlog.RequestUsage{At: at, Model: "claude-sonnet-5", Input: 1, CostUSD: &exact}

	got := usage.SumWeight([]agentlog.RequestUsage{settled}, at, at.Add(time.Minute))
	if got != exact {
		t.Errorf("SumWeight = %v; want the settled request's own exact cost %v", got, exact)
	}
}

func TestWeighOnAMissingDirIsZero(t *testing.T) {
	t.Parallel()

	got, err := usage.Weigh(filepath.Join(t.TempDir(), "nothing"), time.Now(), time.Now())
	if err != nil {
		t.Fatalf("Weigh: %v", err)
	}
	if got != 0 {
		t.Errorf("Weigh on a missing dir = %v; want 0", got)
	}
}
