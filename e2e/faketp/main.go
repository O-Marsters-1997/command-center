// Command faketp stands in for the treepad CLI (tp) in end-to-end tests. It implements the two
// commands the app uses — new and remove — by delegating to real git, so worktree layout and
// branch-deletion refusals are genuine rather than simulated.
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
	force := false
	for _, arg := range args {
		if arg == "--force" {
			force = true
			continue
		}
		branch = arg
	}
	if branch == "" {
		printf(stderr, "faketp remove: usage: faketp remove [--force] <branch>\n")
		return 1
	}

	// Real tp removes the worktree first and only then deletes the branch, so a `git branch -d`
	// refusal on a squash-merged branch leaves the worktree gone and the branch behind. That is
	// surprising but faithful, and the e2e tests depend on it — do not reorder.
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

// siblingPath mirrors treepad's layout: worktrees are siblings of the repo, named
// <repo-slug>-<branch-slug>. The app never recomputes this — it reads
// `git worktree list --porcelain` — but the fake must place them where real tp does.
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

// printf writes to w, discarding the write error: these are diagnostics on a test fake.
func printf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
