package cc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Runner spawns agent processes and reads back their liveness, and signals them dead on
// request. The real implementation is ProcessRunner; the loop's tests substitute a fake so
// liveness, disposition and cancellation are exercised without touching the OS.
type Runner interface {
	Spawn(ctx context.Context, cfg SpawnConfig) (SpawnResult, error)
	Liveness(pgid int, wantStart, now time.Time) (bool, error)
	Cancel(pgid int) error
	// Reap collects a dead process's exit code. ok is false when there is none to report.
	Reap(pid int) (exitCode int, ok bool)
}

// SpawnConfig is everything Spawn needs to start one agent process. AgentCommand is the
// configured argv template; {worktree}, {settings} and {prompt_file} are substituted into every
// element before exec.
type SpawnConfig struct {
	AgentCommand []string
	WorktreePath string
	SettingsPath string
	PromptPath   string
	// LogFile is both stdout and stderr, opened by the caller and never a pipe: piping would
	// need a goroutine per run to drain it, which the design forbids (§3).
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
	arg = strings.ReplaceAll(arg, "{prompt_file}", cfg.PromptPath)
	return arg
}

// stripAPIKey removes ANTHROPIC_API_KEY from an environment list: every agent runs under the
// app-owned settings file instead of inheriting the app's own key.
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

// Spawn starts one agent process as the leader of its own process group, so Cancel can later
// signal every subprocess it spawns, not just itself. It deliberately skips exec.CommandContext:
// since Go 1.20 that kills only the leader pid on ctx cancellation, which is wrong for crash
// recovery — a graceful shutdown must leave agents running exactly like a crash does (§3).
func (ProcessRunner) Spawn(_ context.Context, cfg SpawnConfig) (SpawnResult, error) {
	if len(cfg.AgentCommand) == 0 {
		return SpawnResult{}, fmt.Errorf("spawn agent: agent_command is empty")
	}
	argv := make([]string, len(cfg.AgentCommand))
	for i, a := range cfg.AgentCommand {
		argv[i] = substitute(a, cfg)
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = cfg.LogFile
	cmd.Stderr = cfg.LogFile
	cmd.Env = stripAPIKey(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return SpawnResult{}, fmt.Errorf("spawn agent %s: %w", argv[0], err)
	}
	return SpawnResult{Pid: cmd.Process.Pid}, nil
}
