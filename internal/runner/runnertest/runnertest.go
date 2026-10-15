package runnertest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/runner"
)

const sigtermExit = 143

type process struct {
	alive, reapable bool
	code            int
}

type Runner struct {
	mu        sync.Mutex
	spawns    []runner.SpawnConfig
	canceled  []int
	processes map[int]*process
	lastPid   int
}

var _ runner.Runner = (*Runner)(nil)

func (r *Runner) set(pid int, p process) {
	if r.processes == nil {
		r.processes = map[int]*process{}
	}
	r.processes[pid] = &p
}

func (r *Runner) Run(pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.set(pid, process{alive: true})
}

func (r *Runner) Exit(pid, code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.set(pid, process{reapable: true, code: code})
}

func (r *Runner) Vanish(pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.set(pid, process{})
}

func (r *Runner) Alive(pid int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.processes[pid]
	return ok && p.alive
}

func (r *Runner) Spawns() []runner.SpawnConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.spawns)
}

func (r *Runner) Canceled() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.canceled)
}

func (r *Runner) Spawn(_ context.Context, cfg runner.SpawnConfig) (runner.SpawnResult, error) {
	if len(cfg.AgentCommand) == 0 {
		return runner.SpawnResult{}, errors.New("spawn agent: agent_command is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spawns = append(r.spawns, cfg)
	r.lastPid++
	r.set(r.lastPid, process{alive: true})
	return runner.SpawnResult{Pid: r.lastPid}, nil
}

func (r *Runner) Liveness(pgid int, _, _ time.Time) (bool, error) {
	return r.Alive(pgid), nil
}

func (r *Runner) Cancel(pgid int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.canceled = append(r.canceled, pgid)
	if p, ok := r.processes[pgid]; ok && p.alive {
		r.set(pgid, process{reapable: true, code: sigtermExit})
	}
	return nil
}

func (r *Runner) Reap(pid int) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.processes[pid]
	if !ok || !p.reapable {
		return 0, false
	}
	p.reapable = false
	return p.code, true
}

func RunContract(t *testing.T, newRunner func(t *testing.T) runner.Runner) {
	t.Helper()

	spawn := func(t *testing.T, r runner.Runner) int {
		t.Helper()
		dir := t.TempDir()
		log, err := os.Create(filepath.Join(dir, "agent.log"))
		if err != nil {
			t.Fatalf("create agent log: %v", err)
		}
		t.Cleanup(func() {
			if err := log.Close(); err != nil {
				t.Errorf("close agent log: %v", err)
			}
		})
		res, err := r.Spawn(t.Context(), runner.SpawnConfig{
			AgentCommand: []string{"sleep", "30"}, WorktreePath: dir, LogFile: log,
		})
		if err != nil {
			t.Fatalf("Spawn(sleep 30) error = %v", err)
		}
		return res.Pid
	}

	t.Run("a spawned process is live until cancelled and reaped", func(t *testing.T) {
		t.Parallel()
		r := newRunner(t)
		start := time.Now()
		pid := spawn(t, r)

		if alive, err := r.Liveness(pid, start, time.Now()); err != nil || !alive {
			t.Fatalf("Liveness(spawned) = %v, %v, want true, nil", alive, err)
		}
		if err := r.Cancel(pid); err != nil {
			t.Fatalf("Cancel(spawned) error = %v", err)
		}
		if code, ok := r.Reap(pid); !ok || code == 0 {
			t.Errorf("Reap(cancelled) = %d, %v, want a non-zero exit, true", code, ok)
		}
		if alive, err := r.Liveness(pid, start, time.Now()); err != nil || alive {
			t.Errorf("Liveness(reaped) = %v, %v, want false, nil", alive, err)
		}
	})

	t.Run("Reap of a pid it never spawned", func(t *testing.T) {
		t.Parallel()
		if code, ok := newRunner(t).Reap(1); ok {
			t.Errorf("Reap(1) = %d, true, want false", code)
		}
	})

	t.Run("Spawn with no agent command", func(t *testing.T) {
		t.Parallel()
		if _, err := newRunner(t).Spawn(t.Context(), runner.SpawnConfig{WorktreePath: t.TempDir()}); err == nil {
			t.Error("Spawn(no agent_command) error = nil, want one")
		}
	})
}
