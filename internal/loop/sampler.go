package loop

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/O-Marsters-1997/command-center/internal/adapter/meter"
	"github.com/O-Marsters-1997/command-center/internal/plan/memory"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type Sampler struct {
	Logger *slog.Logger
	Store  *store.Store
	Meter  meter.Meter
	Clock  Clock
}

func (s *Sampler) Run(ctx context.Context) error {
	for {
		if err := s.SampleOnce(ctx); err != nil && ctx.Err() == nil {
			s.Logger.Error("memory sample", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-s.Clock.After(memory.SampleInterval):
		}
	}
}

func (s *Sampler) SampleOnce(ctx context.Context) error {
	runs, err := s.Store.PendingRunsAwaitingDisposition(ctx)
	if err != nil {
		return fmt.Errorf("read live runs: %w", err)
	}
	runByPgid := make(map[int]int64, len(runs))
	pgids := make([]int, 0, len(runs))
	for _, r := range runs {
		runByPgid[r.Pgid] = r.ID
		pgids = append(pgids, r.Pgid)
	}
	reading, err := s.Meter.Read(ctx, pgids)
	if err != nil {
		return fmt.Errorf("read meter: %w", err)
	}
	sample := memory.Sample{At: s.Clock.Now(), Total: reading.Total, Used: reading.Used}
	for _, p := range reading.Procs {
		runID, ok := runByPgid[p.Pgid]
		if !ok {
			continue
		}
		sample.Procs = append(sample.Procs, memory.ProcFact{
			RunID: runID, Pid: p.Pid, Name: p.Name, Args: p.Args, RSS: p.RSS,
		})
	}
	if err := s.Store.RecordMemorySample(ctx, sample); err != nil {
		return fmt.Errorf("record memory sample: %w", err)
	}
	return nil
}
