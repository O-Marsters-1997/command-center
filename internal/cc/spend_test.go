package cc

import (
	"os"
	"path/filepath"
	"testing"
)

const aliveLine = `{"type":"assistant","timestamp":"2026-01-01T00:00:00.000Z","request_id":"r1",` +
	`"message":{"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"

const resultLine = `{"type":"result","subtype":"success","duration_ms":1000,"num_turns":3,"total_cost_usd":1.23}` + "\n"

func writeLog(t *testing.T, lines ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "run.jsonl")
	var content string
	for _, line := range lines {
		content += line
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSpendCacheReportsTokensWhileAliveAndDollarsOnceSettled(t *testing.T) {
	t.Parallel()

	cache := newSpendCache()

	alive := writeLog(t, aliveLine)
	tokens, usd, settled := cache.Spend(alive)
	if tokens != 15 || usd != 0 || settled {
		t.Errorf("Spend(alive) = (%d, %v, %v); want (15, 0, false)", tokens, usd, settled)
	}

	ended := writeLog(t, aliveLine, resultLine)
	tokens, usd, settled = cache.Spend(ended)
	if tokens != 15 || usd != 1.23 || !settled {
		t.Errorf("Spend(ended) = (%d, %v, %v); want (15, 1.23, true)", tokens, usd, settled)
	}
}

func TestSpendCacheReturnsTheZeroSpendForNoLogPath(t *testing.T) {
	t.Parallel()

	cache := newSpendCache()
	tokens, usd, settled := cache.Spend("")
	if tokens != 0 || usd != 0 || settled {
		t.Errorf("Spend(\"\") = (%d, %v, %v); want (0, 0, false)", tokens, usd, settled)
	}
}

func TestSpendCacheNeverRescansASettledLog(t *testing.T) {
	t.Parallel()

	cache := newSpendCache()
	path := writeLog(t, aliveLine, resultLine)

	tokens, usd, settled := cache.Spend(path)
	if !settled {
		t.Fatalf("Spend(path) settled = false, want true")
	}
	if entry := cache.by[path]; entry.reads != 1 {
		t.Fatalf("reads after settling = %d, want 1", entry.reads)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		gotTokens, gotUSD, gotSettled := cache.Spend(path)
		if gotTokens != tokens || gotUSD != usd || !gotSettled {
			t.Errorf("Spend(path) after settling = (%d, %v, %v); want (%d, %v, true)",
				gotTokens, gotUSD, gotSettled, tokens, usd)
		}
	}
	if entry := cache.by[path]; entry.reads != 1 {
		t.Errorf("reads after 5 more calls = %d, want 1 (a settled entry is never rescanned)", entry.reads)
	}
}

func TestSpendCacheTreatsARerunsNewLogPathAsANewEntry(t *testing.T) {
	t.Parallel()

	cache := newSpendCache()
	first := writeLog(t, aliveLine, resultLine)
	cache.Spend(first)

	second := writeLog(t, aliveLine, aliveLine)
	tokens, usd, settled := cache.Spend(second)
	if tokens != 15 || usd != 0 || settled {
		t.Errorf("Spend(second) = (%d, %v, %v); want (15, 0, false): a fresh key, not the first's settled value",
			tokens, usd, settled)
	}
	if entry := cache.by[first].reads; entry != 1 {
		t.Errorf("first's reads = %d, want 1: the second path's own reads must not touch it", entry)
	}
}
