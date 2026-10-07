package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/command"
)

// Fetch updates a repo's remote-tracking refs. --prune because both repos delete branches on
// merge, and a stale ref would make a deleted base look cuttable.
func Fetch(ctx context.Context, repoPath string) error {
	_, err := git(ctx, repoPath, "fetch", "origin", "--prune")
	return err
}

// WorktreePaths reads the branch -> path map from git, including worktrees mid-rebase.
func WorktreePaths(ctx context.Context, repoPath string) (map[string]string, error) {
	out, err := git(ctx, repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	worktrees, detached := parseWorktrees(out)
	for _, path := range detached {
		branch, err := rebasingBranch(ctx, path)
		if err != nil {
			return nil, err
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

// git lists a rebasing worktree as detached, so its branch comes from the rebase state.
func rebasingBranch(ctx context.Context, worktreePath string) (string, error) {
	for _, dir := range rebaseDirs {
		headName, err := gitPath(ctx, worktreePath, dir+"/head-name")
		if err != nil {
			return "", err
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

// --git-path answers relatively in a plain repo and absolutely in a linked worktree.
func gitPath(ctx context.Context, worktreePath, name string) (string, error) {
	out, err := git(ctx, worktreePath, "rev-parse", "--git-path", name)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if filepath.IsAbs(path) {
		return path, nil
	}
	return filepath.Join(worktreePath, path), nil
}

// BranchTip reads a branch's current tip SHA.
func BranchTip(ctx context.Context, repoPath, branch string) (string, error) {
	return RevParse(ctx, repoPath, "refs/heads/"+branch)
}

// DeleteBranchIfExists removes branch's local ref, doing nothing if it has none.
func DeleteBranchIfExists(ctx context.Context, repoPath, branch string) error {
	if _, err := RevParse(ctx, repoPath, "refs/heads/"+branch); err != nil {
		return nil
	}
	_, err := git(ctx, repoPath, "branch", "-D", branch)
	return err
}

// RevParse resolves any ref to its commit SHA.
func RevParse(ctx context.Context, repoPath, ref string) (string, error) {
	out, err := git(ctx, repoPath, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ShowFile reads path's content at ref without touching the working tree.
func ShowFile(ctx context.Context, repoPath, ref, path string) (string, error) {
	out, err := git(ctx, repoPath, "show", ref+":"+path)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ChangedPaths lists the paths branch changed relative to its merge base with base.
func ChangedPaths(ctx context.Context, repoPath, base, branch string) ([]string, error) {
	out, err := git(ctx, repoPath, "diff", "--name-only", base+"..."+branch)
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// Push pushes branch to origin, never forcing.
func Push(ctx context.Context, repoPath, branch string) error {
	_, err := git(ctx, repoPath, "push", "origin", branch)
	return err
}

// PushRestacked pushes a rewritten branch, leasing on expectedRemote so commits that reached
// origin since the last recorded push (a reviewer's suggestion, Mergify) refuse instead of
// being discarded.
func PushRestacked(ctx context.Context, repoPath, branch, expectedRemote string) error {
	lease := "--force-with-lease=" + branch + ":" + expectedRemote
	_, err := git(ctx, repoPath, "push", lease, "origin", branch)
	return err
}

// CommitsSince counts commits reachable from ref but not from baselineSHA.
func CommitsSince(ctx context.Context, repoPath, baselineSHA, ref string) (int, error) {
	out, err := git(ctx, repoPath, "rev-list", "--count", baselineSHA+".."+ref)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parse rev-list --count output %q: %w", out, err)
	}
	return n, nil
}

// RemovalState is which of tp's own removal checks a branch has left to run, or whether cc can
// prove the same thing once delete-branch-on-merge has pruned the ref tp would check against.
type RemovalState int

const (
	// RemovableByMerged: the remote-tracking ref still resolves, so tp remove --merged can
	// still check the branch itself.
	RemovableByMerged RemovalState = iota
	// RemovableByForce: the ref is gone, but the branch sits exactly where this app last
	// pushed it (docs/adr/0008-cc-proves-what-tp-cannot.md).
	RemovableByForce
	// NotRemovable: the ref is gone and the branch has moved past the last recorded push.
	NotRemovable
)

// RemovalStateFor resolves branch's RemovalState.
func RemovalStateFor(ctx context.Context, repoPath, branch, lastPushedTip string) (RemovalState, error) {
	if _, err := RevParse(ctx, repoPath, "refs/remotes/origin/"+branch); err == nil {
		return RemovableByMerged, nil
	}
	local, err := RevParse(ctx, repoPath, "refs/heads/"+branch)
	if err != nil {
		return NotRemovable, err
	}
	if local != lastPushedTip {
		return NotRemovable, nil
	}
	return RemovableByForce, nil
}

// Dirty reports whether worktreePath has any uncommitted change.
func Dirty(ctx context.Context, worktreePath string) (bool, error) {
	out, err := git(ctx, worktreePath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// MergeFFOnly fast-forwards worktreePath's own branch to ref, refusing if that is not possible.
// Reviewers' "commit suggestion" clicks and Mergify's update_method: merge both push to the app's
// head branches, and the app never rewrites history to make a divergent push fit.
func MergeFFOnly(ctx context.Context, worktreePath, ref string) error {
	_, err := git(ctx, worktreePath, "merge", "--ff-only", ref)
	return err
}

// Merge merges ref into worktreePath's own branch. A conflict leaves the worktree mid-merge
// for a human, which MidMerge reads.
func Merge(ctx context.Context, worktreePath, ref string) error {
	_, err := git(ctx, worktreePath, "merge", ref)
	return err
}

// UnmergedPaths lists worktreePath's currently unresolved merge conflicts.
func UnmergedPaths(ctx context.Context, worktreePath string) ([]string, error) {
	out, err := git(ctx, worktreePath, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

func Add(ctx context.Context, worktreePath string, paths []string) error {
	_, err := git(ctx, worktreePath, append([]string{"add", "--"}, paths...)...)
	return err
}

func Commit(ctx context.Context, worktreePath, message string) error {
	_, err := git(ctx, worktreePath, "commit", "-m", message)
	return err
}

func StagedPaths(ctx context.Context, worktreePath string) ([]string, error) {
	out, err := git(ctx, worktreePath, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

// CommitNoEdit commits worktreePath's staged changes under the message git already wrote for
// them, via --no-edit.
func CommitNoEdit(ctx context.Context, worktreePath string) error {
	_, err := git(ctx, worktreePath, "commit", "--no-edit")
	return err
}

// Rebase replays worktreePath's own branch onto onto, dropping every commit reachable from
// upstream, because a squash-merged base has no ancestry to replay the branch's copies against.
// A conflict leaves the worktree mid-rebase, which MidMerge reads.
func Rebase(ctx context.Context, worktreePath, onto, upstream string) error {
	_, err := git(ctx, worktreePath, "rebase", "--onto", onto, upstream)
	return err
}

// MergeAbort undoes an unresolved merge or restack in worktreePath, restoring the pre-merge tip.
func MergeAbort(ctx context.Context, worktreePath string) error {
	rebasing, err := midRebase(ctx, worktreePath)
	if err != nil {
		return err
	}
	if rebasing {
		_, err := git(ctx, worktreePath, "rebase", "--abort")
		return err
	}
	_, err = git(ctx, worktreePath, "merge", "--abort")
	return err
}

// MidMerge reports whether worktreePath is left mid-merge or mid-rebase. It is read, never
// recorded: a human resolving the conflict and committing clears it with no bookkeeping.
func MidMerge(ctx context.Context, worktreePath string) (bool, error) {
	merging, err := refExists(ctx, worktreePath, "MERGE_HEAD")
	if err != nil || merging {
		return merging, err
	}
	return midRebase(ctx, worktreePath)
}

// git leaves REBASE_HEAD behind after `rebase --continue`, so the state directory is the test.
func midRebase(ctx context.Context, worktreePath string) (bool, error) {
	for _, dir := range rebaseDirs {
		path, err := gitPath(ctx, worktreePath, dir)
		if err != nil {
			return false, err
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

// MergesCleanly reports whether merging branch into base is conflict-free, else the conflicted
// paths. merge-tree exits 0 clean and 1 conflicted, but also 1 for an unresolvable ref, so
// both arguments must already resolve.
func MergesCleanly(ctx context.Context, repoPath, base, branch string) (bool, []string, error) {
	out, ok, err := gitRun(ctx, repoPath, "merge-tree", "--write-tree", "--name-only", base, branch)
	if err != nil || ok {
		return ok, nil, err
	}
	return false, conflictedPaths(out), nil
}

// conflictedPaths reads --name-only's own output shape: the result tree's oid on the first
// line, then one conflicted path per line up to the blank line before its diagnostic messages.
func conflictedPaths(out []byte) []string {
	rows := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	var paths []string
	for _, line := range rows[1:] {
		if line == "" {
			break
		}
		paths = append(paths, line)
	}
	return paths
}

// Ancestor reports whether commit is reachable from ref.
func Ancestor(ctx context.Context, repoPath, commit, ref string) (bool, error) {
	return Succeeds(ctx, repoPath, "merge-base", "--is-ancestor", commit, ref)
}

func refExists(ctx context.Context, worktreePath, ref string) (bool, error) {
	return Succeeds(ctx, worktreePath, "rev-parse", "--verify", "-q", ref)
}

// Succeeds runs a git query whose exit status is its answer, so exit 1 is a "no" rather than
// a failure. Every other exit -- 128 for a ref that does not resolve, most of all -- stays an
// error, because a missing commit is not the same answer as a negative one.
func Succeeds(ctx context.Context, repoPath string, args ...string) (bool, error) {
	_, ok, err := gitRun(ctx, repoPath, args...)
	return ok, err
}

func gitRun(ctx context.Context, repoPath string, args ...string) (stdout []byte, ok bool, err error) {
	out, err := git(ctx, repoPath, args...)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return out, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func git(ctx context.Context, repoPath string, args ...string) ([]byte, error) {
	return command.Output(ctx, repoPath, "git", args...)
}

func lines(out []byte) []string {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
