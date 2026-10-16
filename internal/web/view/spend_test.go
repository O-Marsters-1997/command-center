package view_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/web/view"
)

const aliveLine = `{"type":"assistant","timestamp":"2026-01-01T00:00:00.000Z","request_id":"r1",` +
	`"message":{"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"

const resultLine = `{"type":"result","subtype":"success","duration_ms":1000,"num_turns":3,"total_cost_usd":1.23}` + "\n"

func writeRunLog(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q): %v", path, err)
	}
	return path
}

func spendOf(t *testing.T, cache *view.SpendCache, path string) int {
	t.Helper()
	tokens, err := cache.Spend(path)
	if err != nil {
		t.Fatalf("Spend(%q): %v", path, err)
	}
	return tokens
}

func TestSpendCache_Spend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path func(t *testing.T) string
		want int
	}{
		{name: "no log path", path: func(*testing.T) string { return "" }, want: 0},
		{
			name: "a live log",
			path: func(t *testing.T) string {
				t.Helper()
				return writeRunLog(t, aliveLine)
			},
			want: 15,
		},
		{
			name: "a settled log",
			path: func(t *testing.T) string {
				t.Helper()
				return writeRunLog(t, aliveLine+resultLine)
			},
			want: 15,
		},
		{
			name: "a log not yet written",
			path: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "absent.jsonl")
			},
			want: 0,
		},
		{
			name: "every deduplicated request of a recorded log",
			path: func(*testing.T) string {
				return filepath.Join("..", "..", "plan", "agentlog", "testdata", "run27.jsonl")
			},
			want: 611097,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := tt.path(t)
			if got := spendOf(t, view.NewSpendCache(), path); got != tt.want {
				t.Errorf("Spend(%q) = %d, want %d", path, got, tt.want)
			}
		})
	}

	t.Run("a settled log is never rescanned", func(t *testing.T) {
		t.Parallel()
		cache := view.NewSpendCache()
		path := writeRunLog(t, aliveLine+resultLine)
		want := spendOf(t, cache, path)
		if err := os.Remove(path); err != nil {
			t.Fatalf("os.Remove(%q): %v", path, err)
		}

		if got := spendOf(t, cache, path); got != want {
			t.Errorf("Spend(%q) after settling and removing = %d, want %d", path, got, want)
		}
	})

	t.Run("a rerun's new log path is a new entry", func(t *testing.T) {
		t.Parallel()
		cache := view.NewSpendCache()
		spendOf(t, cache, writeRunLog(t, aliveLine+resultLine))
		second := writeRunLog(t, aliveLine+aliveLine)

		if got := spendOf(t, cache, second); got != 15 {
			t.Errorf("Spend(%q) = %d, want 15", second, got)
		}
	})

	t.Run("a live log is reread on every call", func(t *testing.T) {
		t.Parallel()
		cache := view.NewSpendCache()
		path := writeRunLog(t, aliveLine)
		spendOf(t, cache, path)
		appended := `{"type":"assistant","request_id":"r2","message":{"usage":{"input_tokens":1,"output_tokens":2}}}` + "\n"
		if err := os.WriteFile(path, []byte(aliveLine+appended+resultLine), 0o600); err != nil {
			t.Fatalf("os.WriteFile(%q): %v", path, err)
		}

		if got := spendOf(t, cache, path); got != 18 {
			t.Errorf("Spend(%q) after appending = %d, want 18", path, got)
		}
	})

	t.Run("a parse in flight does not block a settled read", func(t *testing.T) {
		t.Parallel()
		cache := view.NewSpendCache()
		settled := writeRunLog(t, aliveLine+resultLine)
		spendOf(t, cache, settled)
		fifo := filepath.Join(t.TempDir(), "live.jsonl")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatalf("Mkfifo(%q): %v", fifo, err)
		}
		parsed := make(chan error, 1)
		go func() {
			_, err := cache.Spend(fifo)
			parsed <- err
		}()
		writer, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			t.Fatalf("open %q for writing: %v", fifo, err)
		}

		got := spendOf(t, cache, settled)

		if _, err := writer.WriteString(aliveLine); err != nil {
			t.Fatalf("write %q: %v", fifo, err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close %q: %v", fifo, err)
		}
		if err := <-parsed; err != nil {
			t.Errorf("Spend(%q) error = %v", fifo, err)
		}
		if got != 15 {
			t.Errorf("Spend(%q) while another log parses = %d, want 15", settled, got)
		}
	})
}

func TestApplyTicketSpend(t *testing.T) {
	t.Parallel()

	rows := []view.Row{{URL: "sandbox://CC-1"}, {URL: "sandbox://CC-2"}, {URL: "sandbox://CC-3"}}
	byURL := map[string]store.BoardTicketSpend{
		"sandbox://CC-1": {AgentUSD: 1, FollowUpUSD: 0.25, Merged: true},
		"sandbox://CC-2": {AgentUSD: 2},
	}

	view.ApplyTicketSpend(rows, byURL, 0.1)

	want := []view.Row{
		{URL: "sandbox://CC-1", AgentPctWeek: 10, FollowUpPctWeek: 2.5, SpendPctWeek: 12.5},
		{URL: "sandbox://CC-2", AgentPctWeek: 20, SpendPctWeek: 20, TicketOpen: true},
		{URL: "sandbox://CC-3"},
	}
	if diff := cmp.Diff(want, rows); diff != "" {
		t.Errorf("ApplyTicketSpend() rows mismatch (-want +got):\n%s", diff)
	}
}
