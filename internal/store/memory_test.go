package store_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/memory"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestStore_MemoryRunPeaks(t *testing.T) {
	t.Parallel()
	store := openStore(t)
	seedOneTicket(t, store)
	ctx := t.Context()
	a, b := insertAgentRun(t, store), insertAgentRun(t, store)
	samples := []memory.Sample{
		{At: testNow, Total: 100, Procs: []memory.ProcFact{{RunID: a, Pid: 1, RSS: 10}, {RunID: a, Pid: 2, RSS: 20}, {RunID: b, Pid: 3, RSS: 5}}},
		{At: testNow.Add(time.Minute), Total: 200, Procs: []memory.ProcFact{{RunID: a, Pid: 1, RSS: 25}}},
	}
	for _, sm := range samples {
		if err := store.RecordMemorySample(ctx, sm); err != nil {
			t.Fatalf("RecordMemorySample: %v", err)
		}
	}

	got, err := store.MemoryRunPeaks(ctx, []int64{a, b, b + 1})
	if err != nil {
		t.Fatalf("MemoryRunPeaks: %v", err)
	}

	want := map[int64]memory.RunPeak{a: {RSS: 30, Total: 100}, b: {RSS: 5, Total: 100}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MemoryRunPeaks mismatch (-want +got):\n%s", diff)
	}
}

func TestStore_MemoryPeaks(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	store := openStore(t)
	seedOneTicket(t, store)
	runID, _, err := store.InsertSessionRunSkeleton(ctx, storepkg.RunSkeleton{
		Ticket: "sandbox://CC-1", Kind: "agent", PromptHash: "hash", Session: storepkg.RunSession{StartNew: true},
	})
	if err != nil {
		t.Fatalf("InsertSessionRunSkeleton: %v", err)
	}
	from := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	w := memory.Window{From: from, To: from.Add(time.Hour)}
	record := func(at time.Time, used int64, procs ...memory.ProcFact) {
		t.Helper()
		if err := store.RecordMemorySample(ctx, memory.Sample{At: at, Total: 100, Used: used, Procs: procs}); err != nil {
			t.Fatalf("RecordMemorySample: %v", err)
		}
	}
	record(from.Add(-time.Second), 99)
	record(from.Add(10*time.Second), 40)
	record(from.Add(40*time.Second), 70, memory.ProcFact{RunID: runID, Pid: 2, Name: "gopls", Args: "-remote", RSS: 30})
	record(from.Add(time.Minute), 50)
	record(w.To, 99)

	peaks, err := store.MemoryPeaks(ctx, w, time.Minute)
	if err != nil {
		t.Fatalf("MemoryPeaks: %v", err)
	}
	if len(peaks) != 2 {
		t.Fatalf("MemoryPeaks returned %d buckets, want 2: %v", len(peaks), peaks)
	}
	for i, wantUsed := range []int64{70, 50} {
		if peaks[i].Used != wantUsed || peaks[i].Total != 100 {
			t.Errorf("bucket %d = %+v, want used %d total 100", i, peaks[i], wantUsed)
		}
	}

	got, err := store.MemoryProcesses(ctx, peaks[0].Sample)
	if err != nil {
		t.Fatalf("MemoryProcesses: %v", err)
	}
	want := []memory.ProcFact{{RunID: runID, Pid: 2, Name: "gopls", Args: "-remote", RSS: 30}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("MemoryProcesses (-want +got):\n%s", diff)
	}
}
