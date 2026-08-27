package agentlog_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/agentlog"
)

func TestAccumulatorSpend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		fixture     string
		wantTokens  int
		wantUSD     float64
		wantSettled bool
	}{
		{
			name:       "an alive run reports tokens and no dollars",
			fixture:    "alive.jsonl",
			wantTokens: 82,
		},
		{
			name:        "a finished run settles to the last result's cost",
			fixture:     "run27.jsonl",
			wantTokens:  611097,
			wantUSD:     8.288799200000001,
			wantSettled: true,
		},
		{
			name:       "only dropped kinds still accrue their tokens",
			fixture:    "dropped.jsonl",
			wantTokens: 1,
		},
		{
			name:       "a truncated final line contributes nothing",
			fixture:    "truncated.jsonl",
			wantTokens: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var acc agentlog.Accumulator
			if err := acc.Advance(filepath.Join("testdata", tt.fixture)); err != nil {
				t.Fatalf("Advance: %v", err)
			}
			tokens, usd, settled := acc.Spend()
			if tokens != tt.wantTokens || usd != tt.wantUSD || settled != tt.wantSettled {
				t.Errorf("Spend() = (%d, %v, %v); want (%d, %v, %v)",
					tokens, usd, settled, tt.wantTokens, tt.wantUSD, tt.wantSettled)
			}
		})
	}
}

func TestAccumulatorAdvanceResumes(t *testing.T) {
	t.Parallel()

	whole, err := os.ReadFile(filepath.Join("testdata", "alive.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lines := bytes.SplitAfter(whole, []byte("\n"))
	settled := bytes.Join(lines[:5], nil)
	partial := lines[5][:40]

	path := filepath.Join(t.TempDir(), "1.jsonl")
	if err := os.WriteFile(path, append(bytes.Clone(settled), partial...), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	var acc agentlog.Accumulator
	if err := acc.Advance(path); err != nil {
		t.Fatalf("first Advance: %v", err)
	}
	if got := acc.Offset(); got != int64(len(settled)) {
		t.Errorf("offset after first Advance = %d; want %d (the partial line is not consumed)",
			got, len(settled))
	}
	if tokens, _, _ := acc.Spend(); tokens != 65 {
		t.Errorf("tokens after first Advance = %d; want 65", tokens)
	}

	if err := os.WriteFile(path, whole, 0o600); err != nil {
		t.Fatalf("rewrite log: %v", err)
	}
	if err := acc.Advance(path); err != nil {
		t.Fatalf("second Advance: %v", err)
	}
	if got := acc.Offset(); got != int64(len(whole)) {
		t.Errorf("offset after second Advance = %d; want %d", got, len(whole))
	}
	if tokens, _, _ := acc.Spend(); tokens != 82 {
		t.Errorf("tokens after second Advance = %d; want 82", tokens)
	}
}

func TestAccumulatorSettlesOnAppendedResult(t *testing.T) {
	t.Parallel()

	whole, err := os.ReadFile(filepath.Join("testdata", "run27.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lines := bytes.SplitAfter(whole, []byte("\n"))
	beforeResults := bytes.Join(lines[:21], nil)

	path := filepath.Join(t.TempDir(), "27.jsonl")
	if err := os.WriteFile(path, beforeResults, 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	var acc agentlog.Accumulator
	if err := acc.Advance(path); err != nil {
		t.Fatalf("first Advance: %v", err)
	}
	if tokens, usd, settled := acc.Spend(); tokens != 611097 || usd != 0 || settled {
		t.Errorf("Spend() before the result = (%d, %v, %v); want (611097, 0, false)", tokens, usd, settled)
	}

	if err := os.WriteFile(path, whole, 0o600); err != nil {
		t.Fatalf("rewrite log: %v", err)
	}
	if err := acc.Advance(path); err != nil {
		t.Fatalf("second Advance: %v", err)
	}
	if tokens, usd, settled := acc.Spend(); tokens != 611097 || usd != 8.288799200000001 || !settled {
		t.Errorf("Spend() after the result = (%d, %v, %v); want (611097, 8.2887992, true)",
			tokens, usd, settled)
	}
}

func TestAccumulatorAdvanceToleratesAnAbsentLog(t *testing.T) {
	t.Parallel()

	var acc agentlog.Accumulator
	if err := acc.Advance(filepath.Join(t.TempDir(), "nothing.jsonl")); err != nil {
		t.Fatalf("Advance on an absent log: %v", err)
	}
	if tokens, usd, settled := acc.Spend(); tokens != 0 || usd != 0 || settled {
		t.Errorf("Spend() = (%d, %v, %v); want the zero spend", tokens, usd, settled)
	}
}
