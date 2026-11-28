package store

import (
	"context"
	"fmt"

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
