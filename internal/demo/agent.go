package demo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/runner"
)

// Agent simulates a run's stream-json log and commits scenario files with real Git.
type Agent struct {
	clock  loop.Clock
	issues []issue
	seed   int64

	mu      sync.Mutex
	runs    map[int]*agentRun
	nextPid int
	spawns  map[int]int
}

type agentRun struct {
	issue     issue
	worktree  string
	logPath   string
	started   time.Time
	turns     int
	tokensIn  int
	tokensOut int
	finished  bool
	attempt   int
	exitCode  int
	rng       *rand.Rand
}

func newAgent(clock loop.Clock, issues []issue, seed int64) *Agent {
	return &Agent{
		clock: clock, issues: issues, runs: map[int]*agentRun{}, spawns: map[int]int{},
		seed: seed,
	}
}

func (a *Agent) Spawn(ctx context.Context, cfg runner.SpawnConfig) (runner.SpawnResult, error) {
	branch, err := git(ctx, cfg.WorktreePath, "branch", "--show-current")
	if err != nil {
		return runner.SpawnResult{}, fmt.Errorf("read current branch of %s: %w", cfg.WorktreePath, err)
	}
	ownerIdx := slices.IndexFunc(a.issues, func(i issue) bool { return i.branch == branch })
	if ownerIdx < 0 {
		return runner.SpawnResult{}, fmt.Errorf("no scenario ticket owns branch %s", branch)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextPid++
	a.spawns[a.issues[ownerIdx].number]++
	owner := a.issues[ownerIdx]
	//nolint:gosec // reproducible demo data, not security
	rng := rand.New(rand.NewPCG(uint64(a.seed), uint64(owner.number)))
	run := &agentRun{
		issue:    owner,
		worktree: cfg.WorktreePath,
		logPath:  cfg.LogFile.Name(),
		started:  a.clock.Now(),
		rng:      rng,
		attempt:  a.spawns[owner.number],
	}
	a.runs[a.nextPid] = run
	if err := run.write(map[string]any{"type": "system", "subtype": "init", "session_id": branch}); err != nil {
		return runner.SpawnResult{}, fmt.Errorf("write init line for %s: %w", branch, err)
	}
	return runner.SpawnResult{Pid: a.nextPid}, nil
}

// Step advances live agent runs to the current simulation time.
func (a *Agent) Step(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.clock.Now()
	for _, pid := range slices.Sorted(maps.Keys(a.runs)) {
		run := a.runs[pid]
		if run.finished {
			continue
		}
		if err := a.turn(run); err != nil {
			return fmt.Errorf("play turn for %s: %w", run.issue.branch, err)
		}
		if now.Before(run.started.Add(run.issue.AgentAfter)) {
			continue
		}
		if err := run.finish(ctx, now); err != nil {
			return fmt.Errorf("finish run for %s: %w", run.issue.branch, err)
		}
	}
	return nil
}

func (*Agent) turn(run *agentRun) error {
	in, out := 200+run.rng.IntN(800), 50+run.rng.IntN(200)
	run.turns++
	run.tokensIn += in
	run.tokensOut += out
	return run.write(map[string]any{
		"type": "assistant", "request_id": fmt.Sprintf("req-%s-%d", run.issue.ID, run.turns),
		"message": map[string]any{
			"model":   "claude-sonnet-5",
			"usage":   map[string]int{"input_tokens": in, "output_tokens": out},
			"content": []map[string]any{{"type": "text", "text": "working on " + run.issue.Title}},
		},
	})
}

func (r *agentRun) finish(ctx context.Context, now time.Time) error {
	if r.issue.Result == resultCrash && r.attempt == 1 {
		r.finished = true
		r.exitCode = 1
		return r.write(map[string]any{"type": "result", "subtype": "error_during_execution", "is_error": true})
	}
	if err := r.commitWork(ctx); err != nil {
		return fmt.Errorf("commit work: %w", err)
	}
	r.finished = true
	return r.write(map[string]any{
		"type": "result", "subtype": "success", "num_turns": r.turns,
		"duration_ms":    now.Sub(r.started).Milliseconds(),
		"total_cost_usd": float64(r.tokensIn+r.tokensOut) / 1e6,
		"usage":          map[string]int{"input_tokens": r.tokensIn, "output_tokens": r.tokensOut},
	})
}

func (r *agentRun) commitWork(ctx context.Context) error {
	content := "package main\n"
	if r.issue.Result == resultConflict {
		content = "package main // " + r.issue.ID + "\n"
	}
	files := make(map[string]string, len(r.issue.Files))
	for _, name := range r.issue.Files {
		files[name] = content
	}
	return commitAll(ctx, r.worktree, "implement "+r.issue.Title, files)
}

func (r *agentRun) write(line map[string]any) error {
	encoded, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("encode log line: %w", err)
	}
	f, err := os.OpenFile(r.logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open log %s: %w", r.logPath, err)
	}
	_, werr := f.Write(append(encoded, '\n'))
	return errors.Join(werr, f.Close())
}

func (a *Agent) Liveness(pgid int, _, _ time.Time) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	run, ok := a.runs[pgid]
	return ok && !run.finished, nil
}

func (a *Agent) Cancel(pgid int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if run, ok := a.runs[pgid]; ok {
		run.finished = true
	}
	return nil
}

func (a *Agent) Reap(pid int) (int, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	run, ok := a.runs[pid]
	if !ok {
		return 0, false
	}
	return run.exitCode, run.finished
}

var _ runner.Runner = (*Agent)(nil)
