package store

import (
	"context"
	"fmt"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan/memory"
	"github.com/O-Marsters-1997/command-center/internal/store/ccdb"
)

type memoryProcRow struct {
	RunID    int64  `json:"run_id"`
	Pid      int    `json:"pid"`
	Name     string `json:"name"`
	Args     string `json:"args"`
	RSSBytes int64  `json:"rss_bytes"`
}

func (s *Store) MemoryPeaks(ctx context.Context, w memory.Window, step time.Duration) ([]memory.BucketPeak, error) {
	rows, err := s.q.MemoryPeaks(ctx, ccdb.MemoryPeaksParams{FromAt: w.From, ToAt: w.To, StepSecs: step.Seconds()})
	if err != nil {
		return nil, fmt.Errorf("select memory peaks: %w", err)
	}
	peaks := make([]memory.BucketPeak, len(rows))
	for i, r := range rows {
		peaks[i] = memory.BucketPeak{
			Start: r.SampledAt, Sample: memory.SampleID(r.ID), Total: r.TotalBytes, Used: r.UsedBytes,
		}
	}
	return peaks, nil
}

func (s *Store) MemoryProcesses(ctx context.Context, id memory.SampleID) ([]memory.ProcFact, error) {
	rows, err := s.q.MemoryProcesses(ctx, int64(id))
	if err != nil {
		return nil, fmt.Errorf("select memory processes: %w", err)
	}
	procs := make([]memory.ProcFact, len(rows))
	for i, r := range rows {
		procs[i] = memory.ProcFact{RunID: r.RunID, Pid: int(r.Pid), Name: r.Name, Args: r.Args, RSS: r.RssBytes}
	}
	return procs, nil
}

func (s *Store) RecordMemorySample(ctx context.Context, sm memory.Sample) error {
	rows := make([]memoryProcRow, len(sm.Procs))
	for i, p := range sm.Procs {
		rows[i] = memoryProcRow{RunID: p.RunID, Pid: p.Pid, Name: p.Name, Args: p.Args, RSSBytes: p.RSS}
	}
	encoded, err := encodeJSON("memory processes", rows)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(q *ccdb.Queries) error {
		id, err := q.InsertMemorySample(ctx, ccdb.InsertMemorySampleParams{
			SampledAt: sm.At.UTC(), TotalBytes: sm.Total, UsedBytes: sm.Used,
		})
		if err != nil {
			return fmt.Errorf("insert memory sample: %w", err)
		}
		if err := q.InsertMemoryProcesses(ctx, ccdb.InsertMemoryProcessesParams{SampleID: id, Procs: encoded}); err != nil {
			return fmt.Errorf("insert memory processes: %w", err)
		}
		return nil
	})
}
