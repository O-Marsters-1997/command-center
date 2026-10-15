package loop_test

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh/ghtest"
	"github.com/O-Marsters-1997/command-center/internal/gittest"
	"github.com/O-Marsters-1997/command-center/internal/loop"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
	"github.com/O-Marsters-1997/command-center/internal/tp"
	"github.com/O-Marsters-1997/command-center/internal/tp/tptest"
	"github.com/O-Marsters-1997/command-center/internal/web"
)

var discardLog = slog.New(slog.DiscardHandler)

func branchRef(repo, branch string) plan.BranchRef { return plan.BranchRef{Repo: repo, Branch: branch} }

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
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(repoPath), err)
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

func cutWorktree(t *testing.T, repoPath, branch string) string {
	t.Helper()
	worktreePath := filepath.Join(t.TempDir(), "wt-"+branch)
	gittest.Run(t, repoPath, "worktree", "add", "-b", branch, worktreePath, "origin/main")
	return worktreePath
}

var errForge = errors.New("forge refused")

type failingCreate struct{ *ghtest.Forge }

func (f failingCreate) Create(_ context.Context, repo, base, body string, draft bool) error {
	f.Record(ghtest.Call{Method: "Create", Repo: repo, Base: base, Body: body, Draft: draft})
	return errForge
}

type failingReady struct{ *ghtest.Forge }

func (f failingReady) Ready(_ context.Context, repo, branch string) error {
	f.Record(ghtest.Call{Method: "Ready", Repo: repo, Branch: branch})
	return fmt.Errorf("fake gh: pr ready failed: %w", errForge)
}

type failingCloseIssue struct{ *ghtest.Forge }

func (f failingCloseIssue) CloseIssue(_ context.Context, repo, issue string) error {
	f.Record(ghtest.Call{Method: "CloseIssue", Repo: repo, Issue: issue})
	return errForge
}

type failingEdit struct{ *ghtest.Forge }

func (f failingEdit) Edit(_ context.Context, repo, branch, base string) error {
	f.Record(ghtest.Call{Method: "Edit", Repo: repo, Branch: branch, Base: base})
	return errForge
}

type failingCut struct{ *tptest.Worktrees }

func (failingCut) New(context.Context, string, string, string) error { return errForge }

type refusingRemove struct {
	*tptest.Worktrees
	err error
}

func (r refusingRemove) Remove(context.Context, string, string, tp.RemoveMode) error { return r.err }

type failingRemove struct{ *tptest.Worktrees }

func (failingRemove) Remove(context.Context, string, string, tp.RemoveMode) error { return errForge }

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
		t.Fatalf("UpsertRepo(%q): %v", name, err)
	}
}

var update = flag.Bool("update", false, "regenerate golden files")

var questionPath = regexp.MustCompile(`<runs>/[0-9]+\.question`)

func assertPromptGolden(t *testing.T, path, prompt, runsDir string) {
	t.Helper()
	normalized := strings.ReplaceAll(prompt, runsDir, "<runs>")
	assertGolden(t, path, []byte(questionPath.ReplaceAllString(normalized, "<runs>/<run>.question")))
}

func assertGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("WriteFile golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile golden (regenerate with -update): %v", err)
	}
	if strings.HasSuffix(path, ".json") {
		got = canonicalJSON(t, got)
		want = canonicalJSON(t, want)
	}
	if diff := cmp.Diff(string(want), string(got)); diff != "" {
		t.Errorf("%s mismatch (-want +got), rerun with -update to accept:\n%s", path, diff)
	}
}

func canonicalJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode golden JSON: %v", err)
	}
	canonical, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		t.Fatalf("encode golden JSON: %v", err)
	}
	return canonical
}

func openServer(st *storepkg.Store, clock loop.Clock, dataDir string, opts ...web.Option) *web.Server {
	deps := web.Deps{Logger: discardLog, Store: st, Clock: clock, DataDir: dataDir}
	return web.NewServer(deps, opts...)
}

func renderPage(t *testing.T, server *web.Server) string {
	t.Helper()
	return renderPath(t, server, "/tickets")
}

func renderPath(t *testing.T, server *web.Server, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200: %s", path, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func runGitAllowingFailure(t *testing.T, dir string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = os.Environ()
	return cmd.CombinedOutput()
}
