package view

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/store"
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

	cache := NewSpendCache()

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

	cache := NewSpendCache()
	tokens, usd, settled := cache.Spend("")
	if tokens != 0 || usd != 0 || settled {
		t.Errorf("Spend(\"\") = (%d, %v, %v); want (0, 0, false)", tokens, usd, settled)
	}
}

func TestSpendCacheNeverRescansASettledLog(t *testing.T) {
	t.Parallel()

	cache := NewSpendCache()
	path := writeLog(t, aliveLine, resultLine)

	tokens, usd, settled := cache.Spend(path)
	if !settled {
		t.Fatalf("Spend(path) settled = false, want true")
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		gotTokens, gotUSD, gotSettled := cache.Spend(path)
		if gotTokens != tokens || gotUSD != usd || !gotSettled {
			t.Errorf("Spend(path) after settling = (%d, %v, %v); want (%d, %v, true): a settled log is never rescanned",
				gotTokens, gotUSD, gotSettled, tokens, usd)
		}
	}
}

func TestSpendCacheTreatsARerunsNewLogPathAsANewEntry(t *testing.T) {
	t.Parallel()

	cache := NewSpendCache()
	first := writeLog(t, aliveLine, resultLine)
	cache.Spend(first)

	second := writeLog(t, aliveLine, aliveLine)
	tokens, usd, settled := cache.Spend(second)
	if tokens != 15 || usd != 0 || settled {
		t.Errorf("Spend(second) = (%d, %v, %v); want (15, 0, false): a fresh key, not the first's settled value",
			tokens, usd, settled)
	}
}

func TestSpendCacheRereadsALiveLogOnEveryCall(t *testing.T) {
	t.Parallel()

	cache := NewSpendCache()
	path := writeLog(t, aliveLine)
	cache.Spend(path)

	appended := `{"type":"assistant","request_id":"r2","message":{"usage":{"input_tokens":1,"output_tokens":2}}}` + "\n"
	if err := os.WriteFile(path, []byte(aliveLine+appended+resultLine), 0o600); err != nil {
		t.Fatal(err)
	}
	tokens, usd, settled := cache.Spend(path)
	if tokens != 18 || usd != 1.23 || !settled {
		t.Errorf("Spend(path) after appending = (%d, %v, %v); want (18, 1.23, true)", tokens, usd, settled)
	}
}

func TestSpendCacheSumsEveryDeduplicatedRequestOfAFixtureLog(t *testing.T) {
	t.Parallel()

	tokens, usd, settled := NewSpendCache().Spend(filepath.Join("..", "..", "agentlog", "testdata", "run27.jsonl"))
	if tokens != 611097 || usd != 8.288799200000001 || !settled {
		t.Errorf("Spend(run27) = (%d, %v, %v); want (611097, 8.288799200000001, true)", tokens, usd, settled)
	}
}

func TestSpendCacheReportsNoSpendForALogNotYetWritten(t *testing.T) {
	t.Parallel()

	tokens, usd, settled := NewSpendCache().Spend(filepath.Join(t.TempDir(), "absent.jsonl"))
	if tokens != 0 || usd != 0 || settled {
		t.Errorf("Spend(absent) = (%d, %v, %v); want (0, 0, false)", tokens, usd, settled)
	}
}

func TestApplyTicketSpendStacksByKindAndFlagsAnOpenTicket(t *testing.T) {
	t.Parallel()

	rows := []Row{{URL: "sandbox://CC-1"}, {URL: "sandbox://CC-2"}}
	byURL := map[string]store.BoardTicketSpend{
		"sandbox://CC-1": {AgentUSD: 1, ResolveUSD: 0.5, FollowUpUSD: 0.25, Merged: true},
		"sandbox://CC-2": {AgentUSD: 2, Merged: false},
	}
	applyTicketSpend(rows, byURL, 0.1)

	merged := rows[0]
	if merged.AgentPctWeek != 10 || merged.ResolvePctWeek != 5 || merged.FollowUpPctWeek != 2.5 {
		t.Errorf("merged pct split = %+v, want 10/5/2.5", merged)
	}
	if merged.SpendPctWeek != 17.5 {
		t.Errorf("merged SpendPctWeek = %v, want 17.5 (the stacked total)", merged.SpendPctWeek)
	}
	if merged.TicketOpen {
		t.Error("merged TicketOpen = true, want false")
	}

	open := rows[1]
	if !open.TicketOpen {
		t.Error("open TicketOpen = false, want true: no pr_merged event")
	}
}

func TestApplyTicketSpendLeavesARowWithNoSpendUntouched(t *testing.T) {
	t.Parallel()

	rows := []Row{{URL: "sandbox://CC-1"}}
	applyTicketSpend(rows, map[string]store.BoardTicketSpend{}, 0.1)

	if got := rows[0]; got.SpendPctWeek != 0 || got.TicketOpen {
		t.Errorf("row = %+v, want the zero value: no entry for this ticket", got)
	}
}
