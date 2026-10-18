package loop_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/adapter/git/gittest"
	"github.com/O-Marsters-1997/command-center/internal/adapter/runner"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func TestWorkflowPass(t *testing.T) {
	t.Parallel()

	f := newLoopFixture(t)
	url := f.Tickets[0].URL
	launch := storepkg.LaunchRequest{Ticket: url, PromptHash: composeHash(url), Group: "g", At: testAt, Workflow: "v1"}
	if err := f.Store.QueueLaunchIntent(t.Context(), launch); err != nil {
		t.Fatalf("QueueLaunchIntent(%q): %v", url, err)
	}
	verify := []string{"just", "test"}
	obs := plan.Observation{
		Settings:  map[string]plan.RepoSettings{"repo": {VerifyCommand: verify}},
		Worktrees: map[plan.BranchRef]string{},
	}
	lp := newLoop(f.Store, observing(obs), fixedClock(testAt), f.Cfg, f.WS, f.Fake,
		loop.WithForge(f.Forge), loop.WithWorktrees(f.Worktrees))
	tick := func(t *testing.T) {
		t.Helper()
		if err := lp.RunOnce(t.Context()); err != nil {
			t.Fatalf("RunOnce() error = %v", err)
		}
	}
	finish := func(t *testing.T, exit int, result string) {
		t.Helper()
		runs := len(f.Fake.Spawns())
		f.Fake.Exit(runs, exit)
		if result == "" {
			return
		}
		path := filepath.Join(f.WS.RunsDir, fmt.Sprintf("%d.jsonl", runs))
		line := fmt.Sprintf("{\"type\":\"result\",\"result\":%q}\n", result)
		if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
			t.Fatalf("WriteFile(%q): %v", path, err)
		}
	}
	lastSpawn := func(t *testing.T, wantSpawns int) runner.SpawnConfig {
		t.Helper()
		spawns := f.Fake.Spawns()
		if len(spawns) != wantSpawns {
			t.Fatalf("spawns = %d, want %d", len(spawns), wantSpawns)
		}
		return spawns[wantSpawns-1]
	}

	tick(t)
	setup := lastSpawn(t, 1)
	if !slices.Equal(setup.AgentCommand, verify) {
		t.Errorf("setup spawned %v, want the repo's verify command %v", setup.AgentCommand, verify)
	}
	obs.Worktrees[branchRef("repo", "cc-1")] = setup.WorktreePath

	finish(t, 0, "")
	tick(t)
	read := lastSpawn(t, 2)
	if read.Model != "haiku" || !slices.Equal(read.Tools, []string{"Read", "Grep", "Glob"}) {
		t.Errorf("read spawned with model %q tools %v, want haiku and Read Grep Glob", read.Model, read.Tools)
	}

	finish(t, 0, "FILES: a.go")
	tick(t)
	implement := lastSpawn(t, 3)
	if implement.Model != "sonnet" || !strings.Contains(implement.Prompt, "FILES: a.go") {
		t.Errorf("implement model %q prompt %q, want sonnet and the read handoff", implement.Model, implement.Prompt)
	}

	gittest.Commit(t, setup.WorktreePath, "a.go", "package a\n")
	finish(t, 0, "")
	tick(t)
	lastSpawn(t, 4)

	checkLog := filepath.Join(f.WS.RunsDir, "4.jsonl")
	if err := os.WriteFile(checkLog, []byte("build broke: undefined x\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(check log): %v", err)
	}
	f.Fake.Exit(4, 1)
	tick(t)
	fix := lastSpawn(t, 5)
	if !slices.Equal(fix.Tools, []string{"Read", "Edit", "Bash"}) || !strings.Contains(fix.Prompt, "undefined x") {
		t.Errorf("fix tools %v prompt %q, want Read Edit Bash and the check output", fix.Tools, fix.Prompt)
	}

	gittest.Commit(t, setup.WorktreePath, "b.go", "package b\n")
	finish(t, 0, "")
	tick(t)
	lastSpawn(t, 6)

	f.Fake.Exit(6, 0)
	tick(t)
	lastSpawn(t, 7)

	finish(t, 0, "CLEAN: nothing to change")
	tick(t)
	pr := lastSpawn(t, 8)
	if !slices.Equal(pr.Tools, []string{"Read"}) {
		t.Errorf("pr tools = %v, want Read", pr.Tools)
	}

	finish(t, 0, "Adds a.go and b.go.")
	tick(t)
	latest := f.Latest(t)[url]
	if latest.Step != "pr" || latest.Outcome != option.Some(plan.OutcomePush) {
		t.Errorf("latest run = step %q outcome %v, want pr push", latest.Step, latest.Outcome)
	}
	creates := f.Forge.Calls("Create")
	if len(creates) != 1 || creates[0].Body != "Adds a.go and b.go." {
		t.Errorf("Create calls = %+v, want one opened with the pr handoff as its body", creates)
	}
}
