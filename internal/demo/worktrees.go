package demo

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/cc"
	"github.com/O-Marsters-1997/command-center/internal/tp"
)

var errCutFailed = errors.New("tp new: scenario says this cut fails")

// Worktrees is the tp.Worktrees seam over real git: it cuts and removes worktrees beside the
// repo's checkout, and fails the cut of any ticket whose script says cut = "fail".
type Worktrees struct {
	issues []issue
}

// NewWorktrees returns the Worktrees playing issues' cut scripts.
func NewWorktrees(issues []issue) *Worktrees { return &Worktrees{issues: issues} }

func (w *Worktrees) New(_ context.Context, repoPath, branch, baseRef string) error {
	if slices.ContainsFunc(w.issues, func(i issue) bool { return i.branch == branch && i.Cut == "fail" }) {
		return errCutFailed
	}
	path := filepath.Join(filepath.Dir(repoPath), "wt-"+branch)
	_, err := git(repoPath, "worktree", "add", "-q", "-b", branch, path, baseRef)
	return err
}

func (*Worktrees) Remove(ctx context.Context, repoPath, branch string, _ tp.RemoveMode) error {
	worktrees, err := cc.Worktrees(ctx, repoPath)
	if err != nil {
		return err
	}
	path, ok := worktrees[branch]
	if !ok {
		return fmt.Errorf("no worktree for branch %s", branch)
	}
	if _, err := git(repoPath, "worktree", "remove", "--force", path); err != nil {
		return err
	}
	_, err = git(repoPath, "branch", "-q", "-D", branch)
	return err
}

var _ tp.Worktrees = (*Worktrees)(nil)

func installFailureScripts(sb *Sandbox, issues []issue) (verifyCommand []string, err error) {
	for _, repo := range sb.repos {
		branches := branchesWhere(issues, repo, func(i issue) bool { return i.Push == "fail" })
		if len(branches) == 0 {
			continue
		}
		hook := fmt.Sprintf(`#!/bin/sh
while read -r _ _ ref; do
	case "$ref" in
	%s) echo "demo: push rejected" >&2; exit 1 ;;
	esac
done
`, refPattern(branches, "refs/heads/"))
		//nolint:gosec // an executable hook
		if err := os.WriteFile(filepath.Join(repo.origin, "hooks", "pre-receive"), []byte(hook), 0o700); err != nil {
			return nil, err
		}
	}

	var failing []string
	for _, repo := range sb.repos {
		failing = append(failing, branchesWhere(issues, repo, func(i issue) bool { return i.Verify == "fail" })...)
	}
	if len(failing) == 0 {
		return nil, nil
	}
	script := fmt.Sprintf(`#!/bin/sh
case "$(git branch --show-current)" in
%s) echo "demo: verification failed"; exit 1 ;;
esac
`, refPattern(failing, ""))
	path := filepath.Join(sb.root, "verify.sh")
	//nolint:gosec // an executable script
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		return nil, err
	}
	return []string{path}, nil
}

func branchesWhere(issues []issue, repo *sandboxRepo, match func(issue) bool) []string {
	var branches []string
	for _, i := range issues {
		if i.repo == repo && match(i) {
			branches = append(branches, i.branch)
		}
	}
	return branches
}

func refPattern(branches []string, prefix string) string {
	patterns := make([]string, len(branches))
	for n, b := range branches {
		patterns[n] = prefix + b
	}
	return strings.Join(patterns, "|")
}
