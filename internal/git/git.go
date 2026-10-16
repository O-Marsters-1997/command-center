package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/command"
)

// Fetch updates remote-tracking refs. GitHub deletes merged branches, so --prune
// prevents a stale ref from making a deleted base look cuttable.
func Fetch(ctx context.Context, repoPath string) error {
	_, err := git(ctx, repoPath, "fetch", "origin", "--prune")
	if err != nil {
		return fmt.Errorf("fetch origin: %w", err)
	}
	return nil
}

// WorktreePaths reads the branch-to-path map, including worktrees mid-rebase.
func WorktreePaths(ctx context.Context, repoPath string) (map[string]string, error) {
	out, err := git(ctx, repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	worktrees, detached := parseWorktrees(out)
	for _, path := range detached {
		branch, err := rebasingBranch(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("read rebasing branch: %w", err)
		}
		if branch != "" {
			worktrees[branch] = path
		}
	}
	return worktrees, nil
}

func parseWorktrees(out []byte) (worktrees map[string]string, detached []string) {
	worktrees = map[string]string{}
	var path string
	for line := range strings.Lines(string(out)) {
		field, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch field {
		case "worktree":
			path = value
		case "branch":
			worktrees[strings.TrimPrefix(value, "refs/heads/")] = path
		case "detached":
			detached = append(detached, path)
		}
	}
	return worktrees, detached
}

// Git lists a rebasing worktree as detached; its branch comes from the rebase state.
func rebasingBranch(ctx context.Context, worktreePath string) (string, error) {
	for _, dir := range rebaseDirs {
		headName, err := gitPath(ctx, worktreePath, dir+"/head-name")
		if err != nil {
			return "", fmt.Errorf("resolve rebase head: %w", err)
		}
		ref, err := os.ReadFile(headName)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read %s for %s: %w", headName, worktreePath, err)
		}
		name := strings.TrimSpace(string(ref))
		if !strings.HasPrefix(name, "refs/heads/") {
			return "", nil
		}
		return strings.TrimPrefix(name, "refs/heads/"), nil
	}
	return "", nil
}

var rebaseDirs = []string{"rebase-merge", "rebase-apply"}

// Git answers --git-path relatively in a plain repo and absolutely in a linked worktree.
func gitPath(ctx context.Context, worktreePath, name string) (string, error) {
	out, err := git(ctx, worktreePath, "rev-parse", "--git-path", name)
	if err != nil {
		return "", fmt.Errorf("resolve git path %s: %w", name, err)
	}
	path := strings.TrimSpace(string(out))
	if filepath.IsAbs(path) {
		return path, nil
	}
	return filepath.Join(worktreePath, path), nil
}

func BranchTip(ctx context.Context, repoPath, branch string) (string, error) {
	return RevParse(ctx, repoPath, "refs/heads/"+branch)
}

func DeleteBranchIfExists(ctx context.Context, repoPath, branch string) error {
	exists, err := refExists(ctx, repoPath, "refs/heads/"+branch)
	if err != nil || !exists {
		if err != nil {
			return fmt.Errorf("check branch %s: %w", branch, err)
		}
		return nil
	}
	_, err = git(ctx, repoPath, "branch", "-D", branch)
	if err != nil {
		return fmt.Errorf("delete branch %s: %w", branch, err)
	}
	return nil
}

func RevParse(ctx context.Context, repoPath, ref string) (string, error) {
	out, err := git(ctx, repoPath, "rev-parse", ref)
	if err != nil {
		return "", fmt.Errorf("resolve ref %s: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func TipIfPresent(ctx context.Context, repoPath, ref string) (sha string, present bool, err error) {
	out, ok, err := gitRun(ctx, repoPath, "rev-parse", "--verify", "-q", ref)
	if err != nil || !ok {
		if err != nil {
			return "", false, fmt.Errorf("resolve tip %s: %w", ref, err)
		}
		return "", false, nil
	}
	return strings.TrimSpace(string(out)), true, nil
}

func ShowFile(ctx context.Context, repoPath, ref, path string) (string, error) {
	out, err := git(ctx, repoPath, "show", ref+":"+path)
	if err != nil {
		return "", fmt.Errorf("show file %s at %s: %w", path, ref, err)
	}
	return string(out), nil
}

// HasFile distinguishes a missing file from an unresolved ref, which Git reports as an error.
func HasFile(ctx context.Context, repoPath, ref, path string) (bool, error) {
	out, err := git(ctx, repoPath, "ls-tree", "--name-only", ref, "--", path)
	if err != nil {
		return false, fmt.Errorf("check file %s at %s: %w", path, ref, err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func ChangedPaths(ctx context.Context, repoPath, base, branch string) ([]string, error) {
	out, err := git(ctx, repoPath, "diff", "--name-only", "--no-renames", base+"..."+branch)
	if err != nil {
		return nil, fmt.Errorf("list changed paths for %s: %w", branch, err)
	}
	return lines(out), nil
}

func Push(ctx context.Context, repoPath, branch string) error {
	_, err := git(ctx, repoPath, "push", "origin", branch)
	if err != nil {
		return fmt.Errorf("push branch %s: %w", branch, err)
	}
	return nil
}

// PushRestacked leases on expectedRemote so reviewer or Mergify pushes since the last
// recorded push are not discarded.
func PushRestacked(ctx context.Context, repoPath, branch, expectedRemote string) error {
	lease := "--force-with-lease=" + branch + ":" + expectedRemote
	_, err := git(ctx, repoPath, "push", lease, "origin", branch)
	if err != nil {
		return fmt.Errorf("push restacked branch %s: %w", branch, err)
	}
	return nil
}

func CommitsSince(ctx context.Context, repoPath, baselineSHA, ref string) (int, error) {
	out, err := git(ctx, repoPath, "rev-list", "--count", baselineSHA+".."+ref)
	if err != nil {
		return 0, fmt.Errorf("count commits since %s: %w", baselineSHA, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parse rev-list --count output %q: %w", out, err)
	}
	return n, nil
}

// RemovalState identifies which worktree removal check can be made after GitHub's
// delete-branch-on-merge may have pruned the remote ref.
type RemovalState int

const (
	RemovalUnknown RemovalState = iota
	RemovableByMerged
	// RemovableByForce applies when GitHub deleted the ref after merging the known head.
	RemovableByForce
	NotRemovable
)

func RemovalStateFor(ctx context.Context, repoPath, branch string, provenTips ...string) (RemovalState, error) {
	tracked, err := refExists(ctx, repoPath, "refs/remotes/origin/"+branch)
	if err != nil {
		return RemovalUnknown, fmt.Errorf("check remote branch %s: %w", branch, err)
	}
	if tracked {
		return RemovableByMerged, nil
	}
	local, err := RevParse(ctx, repoPath, "refs/heads/"+branch)
	if err != nil {
		return RemovalUnknown, fmt.Errorf("resolve local branch %s: %w", branch, err)
	}
	if local == "" || !slices.Contains(provenTips, local) {
		return NotRemovable, nil
	}
	return RemovableByForce, nil
}

func Dirty(ctx context.Context, worktreePath string) (bool, error) {
	out, err := git(ctx, worktreePath, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("check worktree status: %w", err)
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// MergeFFOnly preserves head branches pushed by reviewer suggestions or Mergify's
// update_method: merge rather than rewriting divergent history.
func MergeFFOnly(ctx context.Context, worktreePath, ref string) error {
	_, err := git(ctx, worktreePath, "merge", "--ff-only", ref)
	if err != nil {
		return fmt.Errorf("fast-forward branch to %s: %w", ref, err)
	}
	return nil
}

func Merge(ctx context.Context, worktreePath, ref string) error {
	_, err := git(ctx, worktreePath, "merge", ref)
	if err != nil {
		return fmt.Errorf("merge ref %s: %w", ref, err)
	}
	return nil
}

// Rebase excludes commits reachable from upstream because a squash-merged base
// has no ancestry to replay the branch's copies against.
func Rebase(ctx context.Context, worktreePath, onto, upstream string) error {
	_, err := git(ctx, worktreePath, "rebase", "--onto", onto, upstream)
	if err != nil {
		return fmt.Errorf("rebase branch onto %s: %w", onto, err)
	}
	return nil
}

func MergeAbort(ctx context.Context, worktreePath string) error {
	rebasing, err := midRebase(ctx, worktreePath)
	if err != nil {
		return fmt.Errorf("check rebase state: %w", err)
	}
	if rebasing {
		_, err := git(ctx, worktreePath, "rebase", "--abort")
		if err != nil {
			return fmt.Errorf("abort rebase: %w", err)
		}
		return nil
	}
	_, err = git(ctx, worktreePath, "merge", "--abort")
	if err != nil {
		return fmt.Errorf("abort merge: %w", err)
	}
	return nil
}

func MidMerge(ctx context.Context, worktreePath string) (bool, error) {
	merging, err := refExists(ctx, worktreePath, "MERGE_HEAD")
	if err != nil || merging {
		if err != nil {
			return false, fmt.Errorf("check merge head: %w", err)
		}
		return true, nil
	}
	rebasing, err := midRebase(ctx, worktreePath)
	if err != nil {
		return false, fmt.Errorf("check rebase state: %w", err)
	}
	return rebasing, nil
}

// Git leaves REBASE_HEAD behind after `rebase --continue`; the state directory is the test.
func midRebase(ctx context.Context, worktreePath string) (bool, error) {
	for _, dir := range rebaseDirs {
		path, err := gitPath(ctx, worktreePath, dir)
		if err != nil {
			return false, fmt.Errorf("resolve rebase directory: %w", err)
		}
		switch _, err := os.Stat(path); {
		case err == nil:
			return true, nil
		case errors.Is(err, os.ErrNotExist):
		default:
			return false, fmt.Errorf("stat %s for %s: %w", path, worktreePath, err)
		}
	}
	return false, nil
}

// MergesCleanly requires resolvable refs: Git's merge-tree exits 1 both for
// conflicts and for unresolvable refs.
func MergesCleanly(ctx context.Context, repoPath, base, branch string) (bool, error) {
	_, ok, err := gitRun(ctx, repoPath, "merge-tree", "--write-tree", base, branch)
	if err != nil {
		return false, fmt.Errorf("check merge tree for %s: %w", branch, err)
	}
	return ok, nil
}

func Ancestor(ctx context.Context, repoPath, commit, ref string) (bool, error) {
	return Succeeds(ctx, repoPath, "merge-base", "--is-ancestor", commit, ref)
}

func refExists(ctx context.Context, worktreePath, ref string) (bool, error) {
	return Succeeds(ctx, worktreePath, "rev-parse", "--verify", "-q", ref)
}

// Succeeds maps Git's exit 1 to false; exit 128 for an unresolved ref remains
// an error rather than a negative answer.
func Succeeds(ctx context.Context, repoPath string, args ...string) (bool, error) {
	_, ok, err := gitRun(ctx, repoPath, args...)
	if err != nil {
		return false, fmt.Errorf("run git query: %w", err)
	}
	return ok, nil
}

func gitRun(ctx context.Context, repoPath string, args ...string) (stdout []byte, ok bool, err error) {
	out, err := git(ctx, repoPath, args...)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return out, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("run git status query: %w", err)
	}
	return out, true, nil
}

func git(ctx context.Context, repoPath string, args ...string) ([]byte, error) {
	out, err := command.Output(ctx, repoPath, "git", args...)
	if err != nil {
		return nil, fmt.Errorf("run git in %s: %w", repoPath, err)
	}
	return out, nil
}

func lines(out []byte) []string {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
