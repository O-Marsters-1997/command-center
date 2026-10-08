package loop_test

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/O-Marsters-1997/command-center/internal/cctest"
	"github.com/O-Marsters-1997/command-center/internal/config"
	storepkg "github.com/O-Marsters-1997/command-center/internal/store"
)

func openStore(t *testing.T) *storepkg.Store { return openStoreAt(t, cctest.DSN(t)) }

func openStoreAt(t *testing.T, dsn string) *storepkg.Store {
	t.Helper()
	store, err := storepkg.OpenStore(dsn)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return store
}

func execSQL(t *testing.T, dsn, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func seedUserWithSessions(t *testing.T, dsn string, tokens map[string]time.Time) {
	t.Helper()
	execSQL(t, dsn, `INSERT INTO users (id, email, password_hash, created_at) VALUES (1, 'olly@example.com', 'hash', now())`)
	for token, expiresAt := range tokens {
		execSQL(t, dsn,
			`INSERT INTO sessions (user_id, token_sha, created_at, expires_at) VALUES (1, $1, now(), $2)`,
			token, expiresAt)
	}
}

func sessionTokens(t *testing.T, dsn string) []string {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT token_sha FROM sessions ORDER BY token_sha`)
	if err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var tokens []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			t.Fatalf("scan session token: %v", err)
		}
		tokens = append(tokens, token)
	}
	return tokens
}

// installFakeTp puts a script named tp on PATH that delegates to real git worktree add, so
// internal/git.CLI.New is genuinely exercised. exitCode non-zero simulates `tp new` failing
// (an unresolvable base), matching faketp's own $CC_TP_FAIL behaviour.
func installFakeTp(t *testing.T, fail bool) {
	t.Helper()
	bin := t.TempDir()
	var script string
	if fail {
		script = "#!/bin/sh\necho 'faketp: forced failure' >&2\nexit 1\n"
	} else {
		// argv is `tp new <branch> --base <baseRef>`: $1 is the subcommand, $2 the branch,
		// $4 the base ref.
		script = "#!/bin/sh\n" +
			"set -eu\n" +
			"branch=\"$2\"\n" +
			"base=\"$4\"\n" +
			"path=\"$(cd \"$(dirname \"$PWD\")\" && pwd)/wt-$branch\"\n" +
			"git worktree add -b \"$branch\" \"$path\" \"$base\" >&2\n" +
			"printf '%s\\n' \"$path\"\n"
	}
	if err := os.WriteFile(filepath.Join(bin, "tp"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// repoWithOrigin creates a real repo with a pushed origin/main, the shape tp new --base
// origin/<branch> needs to resolve against.
func repoWithOrigin(t *testing.T) (root, repoPath string) {
	t.Helper()
	root = t.TempDir()
	repoPath = filepath.Join(root, "repo")
	remote := filepath.Join(root, "remote.git")

	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main", repoPath)
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("-C", repoPath, "add", "README.md")
	run("-C", repoPath, "commit", "-q", "-m", "initial")
	run("init", "-q", "-b", "main", "--bare", remote)
	run("-C", repoPath, "remote", "add", "origin", remote)
	run("-C", repoPath, "push", "-q", "-u", "origin", "main")
	run("-C", repoPath, "fetch", "-q", "origin")
	return root, repoPath
}

func testConfigAndWorkspace(
	t *testing.T, root string, maxAgents int, agentCommand []string,
) (config.Config, config.Workspace) {
	t.Helper()
	cfg := config.Config{
		MaxAgents:    maxAgents,
		AgentCommand: agentCommand,
		Repos:        []config.Repo{{Name: "repo", Checkout: filepath.Join(root, "repo"), Stacking: false}},
	}
	ws := config.Workspace{
		RunsDir:      t.TempDir(),
		SettingsPath: filepath.Join(t.TempDir(), "agent.json"),
	}
	return cfg, ws
}

type runMetricsRow struct {
	TokensIn, TokensOut sql.NullInt64
	MetricsSettled      sql.NullBool
}

func readRunMetrics(t *testing.T, dsn string, runID int64) runMetricsRow {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var row runMetricsRow
	err = db.QueryRow(`SELECT tokens_in, tokens_out, metrics_settled FROM runs WHERE id = $1`, runID).
		Scan(&row.TokensIn, &row.TokensOut, &row.MetricsSettled)
	if err != nil {
		t.Fatalf("read run metrics for run %d: %v", runID, err)
	}
	return row
}
