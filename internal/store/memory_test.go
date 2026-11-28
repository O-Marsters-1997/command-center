package store_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/memory"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

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
