package runner

import (
	"context"
	"errors"
	"time"
)

// ErrSpawnFailed reports a configured Fake.Spawn failure.
var ErrSpawnFailed = errors.New("spawn failed")

// Fake is an in-memory Runner for tests.
type Fake struct {
	Spawns   []SpawnConfig
	NextPid  int
	Alive    map[int]bool
	ReapCode map[int]int
	CanReap  map[int]bool
	FailNext bool
	Canceled []int
}

func NewFake() *Fake {
	return &Fake{Alive: map[int]bool{}, ReapCode: map[int]int{}, CanReap: map[int]bool{}}
}

func (f *Fake) Spawn(_ context.Context, cfg SpawnConfig) (SpawnResult, error) {
	f.Spawns = append(f.Spawns, cfg)
	if f.FailNext {
		f.FailNext = false
		return SpawnResult{}, ErrSpawnFailed
	}
	f.NextPid++
	f.Alive[f.NextPid] = true
	return SpawnResult{Pid: f.NextPid}, nil
}

func (f *Fake) Liveness(pgid int, _, _ time.Time) (bool, error) {
	return f.Alive[pgid], nil
}

func (f *Fake) Cancel(pgid int) error {
	f.Alive[pgid] = false
	f.Canceled = append(f.Canceled, pgid)
	return nil
}

func (f *Fake) Reap(pid int) (int, bool) {
	if !f.CanReap[pid] {
		return 0, false
	}
	return f.ReapCode[pid], true
}

var _ Runner = (*Fake)(nil)
