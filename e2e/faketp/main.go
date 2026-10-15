package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if logPath := getenv("CC_TP_LOG"); logPath != "" {
		if err := appendLine(logPath, strings.Join(args, " ")); err != nil {
			printf(stderr, "faketp: %v\n", err)
			return 1
		}
	}
	if getenv("CC_TP_FAIL") != "" {
		printf(stderr, "faketp: CC_TP_FAIL is set\n")
		return 1
	}
	if len(args) == 0 {
		printf(stderr, "faketp: usage: faketp new <branch> --base <ref> | faketp remove [--force] <branch>\n")
		return 1
	}

	root, err := repoRoot()
	if err != nil {
		printf(stderr, "faketp: %v\n", err)
		return 1
	}

	switch args[0] {
	case "new":
		return newWorktree(args[1:], root, stdout, stderr)
	case "remove":
		return removeWorktree(args[1:], root, stdout, stderr)
	default:
		printf(stderr, "faketp: unknown command %q\n", args[0])
		return 1
	}
}

func newWorktree(args []string, root string, stdout, stderr io.Writer) int {
	var branch, base string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--base":
			if i+1 >= len(args) {
				printf(stderr, "faketp new: --base needs a value\n")
				return 1
			}
			base = args[i+1]
			i++
		default:
			branch = args[i]
		}
	}
	if branch == "" || base == "" {
		printf(stderr, "faketp new: usage: faketp new <branch> --base <ref>\n")
		return 1
	}

	path := siblingPath(root, branch)
	if code := gitRun(root, stdout, stderr, "worktree", "add", "-b", branch, path, base); code != 0 {
		return code
	}
	printf(stdout, "%s\n", path)
	return 0
}

func removeWorktree(args []string, root string, stdout, stderr io.Writer) int {
	var branch string
	force, merged := false, false
	for _, arg := range args {
		switch arg {
		case "--force":
			force = true
		case "--merged":
			merged = true
		default:
			branch = arg
		}
	}
	if branch == "" {
		printf(stderr, "faketp remove: usage: faketp remove [--force | --merged] <branch>\n")
		return 1
	}

	// Git's branch -d rejects squash-merged branches; treepad's --merged skips that
	// ancestor check but still refuses dirty worktrees and unpushed commits.
	if merged {
		path := siblingPath(root, branch)
		if code := gitRun(root, stdout, stderr, "worktree", "remove", path); code != 0 {
			return code
		}
		unpushed, code := hasUnpushedCommits(root, branch, stderr)
		if code != 0 {
			return code
		}
		if unpushed {
			printf(stderr, "faketp remove: %s has unpushed commits\n", branch)
			return 1
		}
		return gitRun(root, stdout, stderr, "branch", "-D", branch)
	}

	// Treepad removes the worktree before deleting its branch; on a squash merge,
	// git branch -d may then refuse, leaving the branch but not the worktree.
	worktreeArgs := []string{"worktree", "remove"}
	if force {
		worktreeArgs = append(worktreeArgs, "--force")
	}
	if code := gitRun(root, stdout, stderr, append(worktreeArgs, siblingPath(root, branch))...); code != 0 {
		return code
	}

	deleteFlag := "-d"
	if force {
		deleteFlag = "-D"
	}
	return gitRun(root, stdout, stderr, "branch", deleteFlag, branch)
}

// Treepad treats a missing remote-tracking ref as clean after the remote branch is pruned.
func hasUnpushedCommits(root, branch string, stderr io.Writer) (unpushed bool, code int) {
	remote, err := gitOutput(root, "rev-parse", "--verify", "-q", "refs/remotes/origin/"+branch)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, 0
	}
	if err != nil {
		printf(stderr, "faketp remove: %v\n", err)
		return false, 1
	}
	local, err := gitOutput(root, "rev-parse", "refs/heads/"+branch)
	if err != nil {
		printf(stderr, "faketp remove: %v\n", err)
		return false, 1
	}
	return local != remote, 0
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// Treepad puts worktrees beside the repo as <repo-slug>-<branch-slug>.
func siblingPath(root, branch string) string {
	return filepath.Join(filepath.Dir(root), slug(filepath.Base(root))+"-"+slug(branch))
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("locating repo root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitRun(dir string, stdout, stderr io.Writer, args ...string) int {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		printf(stderr, "faketp: git %s: %v\n", strings.Join(args, " "), err)
		return 1
	}
	return 0
}

func printf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func appendLine(path, line string) (err error) {
	f, openErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if openErr != nil {
		return fmt.Errorf("open %s: %w", path, openErr)
	}
	defer func() { err = errors.Join(err, f.Close()) }()

	if _, writeErr := fmt.Fprintln(f, line); writeErr != nil {
		return fmt.Errorf("append to %s: %w", path, writeErr)
	}
	return nil
}
