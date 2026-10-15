package view

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func TestSpendCacheHoldsLockDuringParse(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	cache := NewSpendCache()
	cache.parse = func(string) (agentlog.RunMetrics, error) {
		close(started)
		<-release
		return agentlog.RunMetrics{}, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := cache.Spend("one")
		done <- err
	}()
	<-started
	if cache.mu.TryLock() {
		cache.mu.Unlock()
		close(release)
		t.Fatal("Spend released the cache mutex during log parsing")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("Spend(one) = %v", err)
	}
}

const aliveLine = `{"type":"assistant","timestamp":"2026-01-01T00:00:00.000Z","request_id":"r1",` +
	`"message":{"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"

const resultLine = `{"type":"result","subtype":"success","duration_ms":1000,"num_turns":3,"total_cost_usd":1.23}` + "\n"

func spendOf(t *testing.T, cache *SpendCache, path string) int {
	t.Helper()

	tokens, err := cache.Spend(path)
	if err != nil {
		t.Fatalf("Spend(%q): %v", path, err)
	}
	return tokens
}

func TestSpendCacheReportsTokensWhetherAliveOrSettled(t *testing.T) {
	t.Parallel()

	cache := NewSpendCache()

	if got := spendOf(t, cache, writeRunLog(t, aliveLine)); got != 15 {
		t.Errorf("Spend(alive) = %d, want 15", got)
	}
	if got := spendOf(t, cache, writeRunLog(t, aliveLine+resultLine)); got != 15 {
		t.Errorf("Spend(ended) = %d, want 15", got)
	}
}

func TestSpendCacheReturnsTheZeroSpendForNoLogPath(t *testing.T) {
	t.Parallel()

	if got := spendOf(t, NewSpendCache(), ""); got != 0 {
		t.Errorf("Spend(\"\") = %d, want 0", got)
	}
}

func TestSpendCacheNeverRescansASettledLog(t *testing.T) {
	t.Parallel()

	cache := NewSpendCache()
	path := writeRunLog(t, aliveLine+resultLine)
	tokens := spendOf(t, cache, path)

	if err := os.Remove(path); err != nil {
		t.Fatalf("os.Remove(%q): %v", path, err)
	}
	for range 5 {
		if got := spendOf(t, cache, path); got != tokens {
			t.Errorf("Spend(path) after settling = %d, want %d: a settled log is never rescanned", got, tokens)
		}
	}
}

func TestSpendCacheTreatsARerunsNewLogPathAsANewEntry(t *testing.T) {
	t.Parallel()

	cache := NewSpendCache()
	spendOf(t, cache, writeRunLog(t, aliveLine+resultLine))

	if got := spendOf(t, cache, writeRunLog(t, aliveLine+aliveLine)); got != 15 {
		t.Errorf("Spend(second) = %d, want 15: a fresh key, not the first's settled value", got)
	}
}

func TestSpendCacheRereadsALiveLogOnEveryCall(t *testing.T) {
	t.Parallel()

	cache := NewSpendCache()
	path := writeRunLog(t, aliveLine)
	spendOf(t, cache, path)

	appended := `{"type":"assistant","request_id":"r2","message":{"usage":{"input_tokens":1,"output_tokens":2}}}` + "\n"
	if err := os.WriteFile(path, []byte(aliveLine+appended+resultLine), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q): %v", path, err)
	}
	if got := spendOf(t, cache, path); got != 18 {
		t.Errorf("Spend(path) after appending = %d, want 18", got)
	}
}

func TestSpendCacheSumsEveryDeduplicatedRequestOfAFixtureLog(t *testing.T) {
	t.Parallel()

	got := spendOf(t, NewSpendCache(), filepath.Join("..", "..", "agentlog", "testdata", "run27.jsonl"))
	if got != 611097 {
		t.Errorf("Spend(run27) = %d, want 611097", got)
	}
}

func TestSpendCacheReportsNoSpendForALogNotYetWritten(t *testing.T) {
	t.Parallel()

	if got := spendOf(t, NewSpendCache(), filepath.Join(t.TempDir(), "absent.jsonl")); got != 0 {
		t.Errorf("Spend(absent) = %d, want 0", got)
	}
}

func TestApplyTicketSpendStacksByKindAndFlagsAnOpenTicket(t *testing.T) {
	t.Parallel()

	rows := []Row{{URL: "sandbox://CC-1"}, {URL: "sandbox://CC-2"}}
	byURL := map[string]store.BoardTicketSpend{
		"sandbox://CC-1": {AgentUSD: 1, FollowUpUSD: 0.25, Merged: true},
		"sandbox://CC-2": {AgentUSD: 2, Merged: false},
	}
	applyTicketSpend(rows, byURL, 0.1)

	merged := rows[0]
	if merged.AgentPctWeek != 10 || merged.FollowUpPctWeek != 2.5 {
		t.Errorf("merged pct split = %+v, want 10/2.5", merged)
	}
	if merged.SpendPctWeek != 12.5 {
		t.Errorf("merged SpendPctWeek = %v, want 12.5 (the stacked total)", merged.SpendPctWeek)
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
