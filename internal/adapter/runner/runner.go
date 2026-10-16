package runner

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/option"
)

type Runner interface {
	Spawn(ctx context.Context, cfg SpawnConfig) (SpawnResult, error)
	Liveness(pgid int, wantStart, now time.Time) (bool, error)
	Cancel(pgid int) error
	Reap(pid int) (exitCode int, ok bool)
}

// SpawnConfig supplies an agent command and its process environment.
// AgentCommand substitutes {worktree}, {settings}, {system_prompt}, {model},
// {prompt} and {prompt_file} in each element before exec.
type SpawnConfig struct {
	AgentCommand     []string
	WorktreePath     string
	SettingsPath     string
	SystemPromptPath string
	Model            string
	Effort           option.Option[plan.Effort]
	Prompt           string
	PromptPath       string
	SessionID        string
	Resume           bool
	LogFile          *os.File
}

type SpawnResult struct {
	Pid int
}

type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type Process struct {
	Zombie  bool
	Elapsed time.Duration
}

type ProcessTable interface {
	Lookup(pid int) (p Process, found bool, err error)
}

// ProcessRunner runs agent processes on Unix.
type ProcessRunner struct {
	Clock     Clock
	Processes ProcessTable
}

func substitute(arg string, cfg SpawnConfig) string {
	arg = strings.ReplaceAll(arg, "{worktree}", cfg.WorktreePath)
	arg = strings.ReplaceAll(arg, "{settings}", cfg.SettingsPath)
	arg = strings.ReplaceAll(arg, "{system_prompt}", cfg.SystemPromptPath)
	arg = strings.ReplaceAll(arg, "{model}", cfg.Model)
	arg = strings.ReplaceAll(arg, "{effort}", string(cfg.Effort.UnwrapOr("")))
	arg = strings.ReplaceAll(arg, "{prompt_file}", cfg.PromptPath)
	arg = strings.ReplaceAll(arg, "{prompt}", cfg.Prompt)
	return arg
}

func sessionArgs(cfg SpawnConfig) []string {
	switch {
	case cfg.SessionID == "":
		return nil
	case cfg.Resume:
		return []string{"--resume", cfg.SessionID}
	default:
		return []string{"--session-id", cfg.SessionID}
	}
}

func buildArgv(cfg SpawnConfig) []string {
	omitWhenEmpty := map[string]string{
		"{system_prompt}": cfg.SystemPromptPath,
		"{effort}":        string(cfg.Effort.UnwrapOr("")),
	}
	argv := make([]string, 0, len(cfg.AgentCommand))
	for _, a := range cfg.AgentCommand {
		if a == "{session}" {
			argv = append(argv, sessionArgs(cfg)...)
			continue
		}
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
