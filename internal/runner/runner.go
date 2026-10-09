package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Runner spawns agent processes, reads back their liveness and signals them dead. ProcessRunner
// is the real one; Fake stands in for tests.
type Runner interface {
	Spawn(ctx context.Context, cfg SpawnConfig) (SpawnResult, error)
	Liveness(pgid int, wantStart, now time.Time) (bool, error)
	Cancel(pgid int) error
	// Reap collects a dead process's exit code. ok is false when there is none to report.
	Reap(pid int) (exitCode int, ok bool)
}

// SpawnConfig is everything Spawn needs to start one agent process. AgentCommand is the
// configured argv template; {worktree}, {settings}, {system_prompt}, {agents}, {model}, {prompt} and
// {prompt_file} are substituted into every element before exec.
type SpawnConfig struct {
	AgentCommand     []string
	WorktreePath     string
	SettingsPath     string
	SystemPromptPath string
	AgentsPath       string
	Model            string
	Prompt           string
	PromptPath       string
	// LogFile is both stdout and stderr and is never a pipe, which would need a draining goroutine.
	LogFile *os.File
}

// SpawnResult is what the caller learns from a successful Spawn.
type SpawnResult struct {
	Pid int
}

// ProcessRunner is the real Runner, spawning and signalling OS processes.
type ProcessRunner struct{}

func substitute(arg string, cfg SpawnConfig) string {
	arg = strings.ReplaceAll(arg, "{worktree}", cfg.WorktreePath)
	arg = strings.ReplaceAll(arg, "{settings}", cfg.SettingsPath)
	arg = strings.ReplaceAll(arg, "{system_prompt}", cfg.SystemPromptPath)
	arg = strings.ReplaceAll(arg, "{agents}", cfg.AgentsPath)
	arg = strings.ReplaceAll(arg, "{model}", cfg.Model)
	arg = strings.ReplaceAll(arg, "{prompt_file}", cfg.PromptPath)
	arg = strings.ReplaceAll(arg, "{prompt}", cfg.Prompt)
	return arg
}

func buildArgv(cfg SpawnConfig) []string {
	omitWhenEmpty := map[string]string{
		"{system_prompt}": cfg.SystemPromptPath,
		"{agents}":        cfg.AgentsPath,
	}
	argv := make([]string, 0, len(cfg.AgentCommand))
	for _, a := range cfg.AgentCommand {
		if path, ok := omitWhenEmpty[a]; ok && path == "" {
			if len(argv) > 0 {
				argv = argv[:len(argv)-1]
			}
			continue
		}
		argv = append(argv, substitute(a, cfg))
	}
	return argv
}

func stripAPIKey(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Spawn starts one agent process as its own process group leader, so Cancel reaches its
// subprocesses. exec.CommandContext is avoided: since Go 1.20 it kills only the leader pid on
// ctx cancellation, but a graceful shutdown must leave agents running as a crash does.
func (ProcessRunner) Spawn(_ context.Context, cfg SpawnConfig) (SpawnResult, error) {
	if len(cfg.AgentCommand) == 0 {
		return SpawnResult{}, fmt.Errorf("spawn agent: agent_command is empty")
	}
	argv := buildArgv(cfg)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cfg.WorktreePath
	cmd.Stdout = cfg.LogFile
	cmd.Stderr = cfg.LogFile
	cmd.Env = stripAPIKey(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return SpawnResult{}, fmt.Errorf("spawn agent %s: %w", argv[0], err)
	}
	return SpawnResult{Pid: cmd.Process.Pid}, nil
}
