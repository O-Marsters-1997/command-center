package runner

import (
	"context"
	"errors"
	"time"
)

// ErrSpawnFailed is what Fake.Spawn returns when FailNext is set.
var ErrSpawnFailed = errors.New("spawn failed")

// Fake is a Runner that never touches the OS. Spawn hands out sequential pids; Liveness and
// Reap answer from the maps a test controls directly.
type Fake struct {
	Spawns   []SpawnConfig
	NextPid  int
	Alive    map[int]bool
	ReapCode map[int]int
	CanReap  map[int]bool
	FailNext bool
	Canceled []int
}

// NewFake returns a Fake with empty state.
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
