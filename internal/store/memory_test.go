package store_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/plan/memory"
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
