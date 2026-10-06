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
	"strings"
	"sync"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/cc"
)

const resolvePromptPrefix = "Merge origin/main into"

// Agent is the fake agent Runner. A spawned run writes stream-json to its log as sim time passes
// and, when its scripted duration is up, commits the scenario's files into the worktree with real
// git.
type Agent struct {
	clock  cc.Clock
	issues []issue
	seed   int64

	mu      sync.Mutex
	runs    map[int]*agentRun
	nextPid int
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
	resolving bool
	rng       *rand.Rand
}

// NewAgent returns a Runner whose token counts come from seed.
func NewAgent(clock cc.Clock, issues []issue, seed int64) *Agent {
	return &Agent{
		clock: clock, issues: issues, runs: map[int]*agentRun{},
		seed: seed,
	}
}

func (a *Agent) Spawn(_ context.Context, cfg cc.SpawnConfig) (cc.SpawnResult, error) {
	branch, err := git(cfg.WorktreePath, "branch", "--show-current")
	if err != nil {
		return cc.SpawnResult{}, err
	}
	ownerIdx := slices.IndexFunc(a.issues, func(i issue) bool { return i.branch == branch })
	if ownerIdx < 0 {
		return cc.SpawnResult{}, fmt.Errorf("no scenario ticket owns branch %s", branch)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextPid++
	owner := a.issues[ownerIdx]
	//nolint:gosec // reproducible demo data, not security
	rng := rand.New(rand.NewPCG(uint64(a.seed), uint64(owner.number)))
	run := &agentRun{
		issue: owner, worktree: cfg.WorktreePath, logPath: cfg.LogFile.Name(), started: a.clock.Now(), rng: rng,
		resolving: strings.HasPrefix(cfg.Prompt, resolvePromptPrefix),
	}
	a.runs[a.nextPid] = run
	if err := run.write(map[string]any{"type": "system", "subtype": "init", "session_id": branch}); err != nil {
		return cc.SpawnResult{}, err
	}
	return cc.SpawnResult{Pid: a.nextPid}, nil
}

// Step plays every live run up to the current sim time: one more assistant turn each, and the
// commit and result line for a run whose time is up.
func (a *Agent) Step() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.clock.Now()
	for _, pid := range slices.Sorted(maps.Keys(a.runs)) {
		run := a.runs[pid]
		if run.finished {
			continue
		}
		if err := a.turn(run); err != nil {
			return err
		}
		if now.Before(run.started.Add(time.Duration(run.issue.Agent.After))) {
			continue
		}
		if err := run.finish(now); err != nil {
			return err
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

func (r *agentRun) finish(now time.Time) error {
	if err := r.commitWork(); err != nil {
		return err
	}
	r.finished = true
	return r.write(map[string]any{
		"type": "result", "subtype": "success", "num_turns": r.turns,
		"duration_ms":    now.Sub(r.started).Milliseconds(),
		"total_cost_usd": float64(r.tokensIn+r.tokensOut) / 1e6,
		"usage":          map[string]int{"input_tokens": r.tokensIn, "output_tokens": r.tokensOut},
	})
}

func (r *agentRun) commitWork() error {
	if r.resolving {
		return r.resolveConflict()
	}
	content := "package main\n"
	if r.issue.Agent.Result == "conflict" {
		content = "package main // " + r.issue.ID + "\n"
	}
	files := make(map[string]string, len(r.issue.Agent.Files))
	for _, name := range r.issue.Agent.Files {
		files[name] = content
	}
	return commitAll(r.worktree, "implement "+r.issue.Title, files)
}

func (r *agentRun) resolveConflict() error {
	if _, err := git(r.worktree, "fetch", "-q", "origin"); err != nil {
		return err
	}
	// The merge exits non-zero precisely when it leaves conflicts to resolve.
	_, _ = git(r.worktree, "merge", "--no-commit", "--no-ff", "origin/main")
	conflicted, err := git(r.worktree, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return err
	}
	resolved := map[string]string{}
	for name := range strings.FieldsSeq(conflicted) {
		resolved[name] = "package main // resolved by " + r.issue.ID + "\n"
	}
	if err := writeFiles(r.worktree, resolved); err != nil {
		return err
	}
	_, err = git(r.worktree, "add", "-A")
	return err
}

func (r *agentRun) write(line map[string]any) error {
	encoded, err := json.Marshal(line)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(r.logPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
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
	return 0, ok && run.finished
}

var _ cc.Runner = (*Agent)(nil)
