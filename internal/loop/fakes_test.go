package loop_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tp"
)

func openStore(t *testing.T) *storepkg.Store {
	t.Helper()
	return cctest.Store(t)
}

func repoWithOrigin(t *testing.T) (root, repoPath string) {
	t.Helper()
	root = t.TempDir()
	repoPath = config.CheckoutPath(root, "repo")
	remote := filepath.Join(root, "remote.git")
	seed := gittest.Init(t)
	gittest.Run(t, ".", "init", "-q", "-b", "main", "--bare", remote)
	if err := os.MkdirAll(filepath.Dir(repoPath), 0o700); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, ".", "clone", "-q", seed, repoPath)
	for key, value := range map[string]string{"user.name": "t", "user.email": "t@example.com", "commit.gpgsign": "false"} {
		gittest.Run(t, repoPath, "config", "--local", key, value)
	}
	gittest.Run(t, repoPath, "remote", "set-url", "origin", remote)
	gittest.Run(t, repoPath, "push", "-q", "-u", "origin", "main")
	gittest.Run(t, repoPath, "fetch", "-q", "origin")
	return root, repoPath
}

type forgeCall struct {
	Method, Repo, Branch, Base, Body, Issue string
	Draft                                   bool
}

type recordingForge struct {
	mu       sync.Mutex
	calls    []forgeCall
	Snapshot gh.Snapshot
	Titles   map[string]string
}

func (f *recordingForge) Calls(method string) []forgeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var calls []forgeCall
	for _, call := range f.calls {
		if call.Method == method {
			calls = append(calls, call)
		}
	}
	return calls
}

func (f *recordingForge) record(call forgeCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *recordingForge) List(_ context.Context, repo string, _ []string) (gh.Snapshot, error) {
	f.record(forgeCall{Method: "List", Repo: repo})
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Snapshot, nil
}

func (f *recordingForge) IssueTitles(_ context.Context, repo string) (map[string]string, error) {
	f.record(forgeCall{Method: "IssueTitles", Repo: repo})
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Titles, nil
}

func (f *recordingForge) Create(_ context.Context, repo, base, body string, draft bool) error {
	f.record(forgeCall{Method: "Create", Repo: repo, Base: base, Body: body, Draft: draft})
	return nil
}

func (f *recordingForge) Ready(_ context.Context, repo, branch string) error {
	f.record(forgeCall{Method: "Ready", Repo: repo, Branch: branch})
	return nil
}

func (f *recordingForge) Edit(_ context.Context, repo, branch, base string) error {
	f.record(forgeCall{Method: "Edit", Repo: repo, Branch: branch, Base: base})
	return nil
}

func (f *recordingForge) CloseIssue(_ context.Context, repo, issue string) error {
	f.record(forgeCall{Method: "CloseIssue", Repo: repo, Issue: issue})
	return nil
}

func (f *recordingForge) Comment(_ context.Context, repo, issue, body string) error {
	f.record(forgeCall{Method: "Comment", Repo: repo, Issue: issue, Body: body})
	return nil
}

var errForge = errors.New("forge refused")

type failingCreate struct{ *recordingForge }

func (f failingCreate) Create(_ context.Context, repo, base, body string, draft bool) error {
	f.record(forgeCall{Method: "Create", Repo: repo, Base: base, Body: body, Draft: draft})
	return errForge
}

type failingReady struct{ *recordingForge }

func (f failingReady) Ready(_ context.Context, repo, branch string) error {
	f.record(forgeCall{Method: "Ready", Repo: repo, Branch: branch})
	return fmt.Errorf("fake gh: pr ready failed: %w", errForge)
}

type failingCloseIssue struct{ *recordingForge }

func (f failingCloseIssue) CloseIssue(_ context.Context, repo, issue string) error {
	f.record(forgeCall{Method: "CloseIssue", Repo: repo, Issue: issue})
	return errForge
}

type recordingWorktrees struct {
	mu      sync.Mutex
	Cut     []string
	Removed []string
}

func (w *recordingWorktrees) New(ctx context.Context, repo, branch, base string) error {
	path := filepath.Join(filepath.Dir(repo), "wt-"+branch)
	if err := worktreeGit(ctx, repo, "worktree", "add", "-b", branch, path, base); err != nil {
		return err
	}
	w.mu.Lock()
	w.Cut = append(w.Cut, branch)
	w.mu.Unlock()
	return nil
}

func (w *recordingWorktrees) Remove(ctx context.Context, repo, branch string, mode tp.RemoveMode) error {
	paths, err := git.WorktreePaths(ctx, repo)
	if err != nil {
		return err
	}
	path, ok := paths[branch]
	if !ok {
		return errors.New("no worktree for " + branch)
	}
	if mode != tp.RemoveForced {
		out, err := worktreeGitOutput(ctx, path, "status", "--porcelain")
		if err != nil {
			return err
		}
		if out != "" {
			return errors.New("worktree is dirty")
		}
		local, err := worktreeGitOutput(ctx, repo, "rev-parse", "refs/heads/"+branch)
		if err != nil {
			return err
		}
		remote, err := worktreeGitOutput(ctx, repo, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
		var exit *exec.ExitError
		if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != 1) {
			return err
		}
		if err == nil && local != remote {
			return errors.New("branch has unpushed commits")
		}
	}
	if err := worktreeGit(ctx, repo, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	if err := worktreeGit(ctx, repo, "branch", "-D", branch); err != nil {
		return err
	}
	w.mu.Lock()
	w.Removed = append(w.Removed, branch)
	w.mu.Unlock()
	return nil
}

type failingCut struct{ *recordingWorktrees }
type failingRemove struct{ *recordingWorktrees }

func (w failingCut) New(context.Context, string, string, string) error              { return errForge }
func (w failingRemove) Remove(context.Context, string, string, tp.RemoveMode) error { return errForge }

func worktreeGit(ctx context.Context, repo string, args ...string) error {
	_, err := worktreeGitOutput(ctx, repo, args...)
	return err
}

func worktreeGitOutput(ctx context.Context, repo string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repo
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %v in %s: %w: %s", args, repo, err, stderr.String())
	}
	return strings.TrimSpace(string(out)), nil
}

func testConfigAndWorkspace(
	t *testing.T, root string, maxAgents int, agentCommand []string,
) (config.Config, config.Workspace) {
	t.Helper()
	cfg := config.Config{DataDir: root, MaxAgents: maxAgents, AgentCommand: agentCommand}
	ws := config.Workspace{
		RunsDir:      t.TempDir(),
		SettingsPath: filepath.Join(t.TempDir(), "agent.json"),
	}
	return cfg, ws
}

func pushSettingsFile(t *testing.T, repoPath, body string) {
	t.Helper()
	gittest.Commit(t, repoPath, config.SettingsFile, body)
	gittest.Run(t, repoPath, "push", "-q", "origin", "main")
}

func trackRepo(t *testing.T, store *storepkg.Store, name, remote string) {
	t.Helper()
	repo := storepkg.Repo{Name: name, Remote: remote, Phase: plan.RepoReady, TrackedAt: testAt}
	if err := store.UpsertRepo(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
}
