package cc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Fetch updates a repo's remote-tracking refs. --prune because both repos delete branches on
// merge, and a stale ref would make a deleted base look cuttable.
func Fetch(ctx context.Context, repoPath string) error {
	if _, err := git(ctx, repoPath, "fetch", "origin", "--prune"); err != nil {
		return err
	}
	return nil
}

// Worktrees reads the authoritative branch -> path map (inv. 5: never a reimplemented
// derivation of where treepad puts things).
func Worktrees(ctx context.Context, repoPath string) (map[string]string, error) {
	out, err := git(ctx, repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}

func parseWorktrees(out []byte) map[string]string {
	worktrees := map[string]string{}
	var path string
	for line := range strings.Lines(string(out)) {
		field, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch field {
		case "worktree":
			path = value
		case "branch":
			worktrees[strings.TrimPrefix(value, "refs/heads/")] = path
		}
	}
	return worktrees
}

// BranchTip reads a branch's current tip SHA, from the shared object database so it resolves
// equally well from the main checkout or from any of its worktrees.
func BranchTip(ctx context.Context, repoPath, branch string) (string, error) {
	return RevParse(ctx, repoPath, "refs/heads/"+branch)
}

// RevParse resolves any ref to its commit SHA -- BranchTip's underlying primitive, reused for
// pushes.base_sha_at_push, whose ref is a remote-tracking branch (origin/<base>), not a local
// one.
func RevParse(ctx context.Context, repoPath, ref string) (string, error) {
	out, err := git(ctx, repoPath, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ShowFile reads path's content at ref without touching the working tree -- the retirement
// pointer's read (docs/designs/command-centre-design.md § 6 job 3).
func ShowFile(ctx context.Context, repoPath, ref, path string) (string, error) {
	out, err := git(ctx, repoPath, "show", ref+":"+path)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ChangedPaths lists the paths base and branch differ on -- the diff the push policy is
// evaluated against (docs/prds/prd-command-centre.md § Phase 4). Three dots diffs against the merge
// base, so only what branch itself added over base is named.
func ChangedPaths(ctx context.Context, repoPath, base, branch string) ([]string, error) {
	out, err := git(ctx, repoPath, "diff", "--name-only", base+"..."+branch)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

// Push pushes branch to origin. Never force: a refused diff never reaches this call, and a
// non-fast-forward here is a genuine push failed, not something to override.
func Push(ctx context.Context, repoPath, branch string) error {
	_, err := git(ctx, repoPath, "push", "origin", branch)
	return err
}

// CommitsSince counts commits reachable from worktreePath's HEAD but not from baselineSHA — a
// dead run's disposition rests on this count, never on missing events (inv. 7).
func CommitsSince(ctx context.Context, worktreePath, baselineSHA string) (int, error) {
	out, err := git(ctx, worktreePath, "rev-list", "--count", baselineSHA+"..HEAD")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parse rev-list --count output %q: %w", out, err)
	}
	return n, nil
}

// IsDirty reports whether worktreePath has any uncommitted change, tracked or untracked --
// `remove worktree`'s first refusal (inv. 3): the app never removes a worktree with anything
// in it a `git status` would show.
func IsDirty(ctx context.Context, worktreePath string) (bool, error) {
	out, err := git(ctx, worktreePath, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

// HasUnpushedCommits reports whether branch's local tip is ahead of its remote-tracking ref --
// `remove worktree`'s second refusal (inv. 3).
//
// lastPushedTip is the pushed_tip this app last recorded for the branch, or "" if it never
// pushed one. It is the fallback when the remote-tracking ref will not resolve, which covers
// two different situations: a branch that was never pushed, and a branch that was pushed,
// merged, deleted by GitHub's delete-branch-on-merge, and then pruned from under us by our own
// `git fetch origin --prune`. Both real repos set delete-branch-on-merge, so without this the
// merged-to-teardown path refuses forever on every squash-merged branch.
// ponytail: a never-pushed branch still always reads as unpushed (lastPushedTip is "") -- a
// deliberate conservative refusal, since remove worktree is destructive.
func HasUnpushedCommits(ctx context.Context, repoPath, branch, lastPushedTip string) (bool, error) {
	local, err := RevParse(ctx, repoPath, "refs/heads/"+branch)
	if err != nil {
		return false, err
	}
	remote, err := RevParse(ctx, repoPath, "refs/remotes/origin/"+branch)
	if err != nil {
		return local != lastPushedTip, nil
	}
	return local != remote, nil
}

// MergeFFOnly fast-forwards worktreePath's own branch to ref, refusing if that is not possible.
// Reviewers' "commit suggestion" clicks and Mergify's update_method: merge both push to the app's
// head branches, and the app never rewrites history to make a divergent push fit (§ 4a).
func MergeFFOnly(ctx context.Context, worktreePath, ref string) error {
	_, err := git(ctx, worktreePath, "merge", "--ff-only", ref)
	return err
}

// Merge merges ref into worktreePath's own branch -- refresh's own step 3 (§4a). A conflict
// leaves the worktree mid-merge for a human, which is what the next tick's MidMerge read derives
// `refresh conflicted` from.
func Merge(ctx context.Context, worktreePath, ref string) error {
	_, err := git(ctx, worktreePath, "merge", ref)
	return err
}

// MergeAbort undoes an unresolved merge in worktreePath, restoring the pre-merge tip --
// `refresh conflicted`'s only verb (docs/designs/command-centre-design.md § 4a).
func MergeAbort(ctx context.Context, worktreePath string) error {
	_, err := git(ctx, worktreePath, "merge", "--abort")
	return err
}

// MidMerge reports whether worktreePath is left mid-merge, which a resolving MERGE_HEAD means.
// It is read, never recorded: a human resolving the conflict and committing clears it with no
// bookkeeping (docs/designs/command-centre-design.md § 4a).
func MidMerge(ctx context.Context, worktreePath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath, "rev-parse", "--verify", "-q", "MERGE_HEAD")
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, fmt.Errorf("check MERGE_HEAD in %s: %w", worktreePath, err)
	}
	return true, nil
}

func git(ctx context.Context, repoPath string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoPath}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s in %s: %w: %s",
			strings.Join(args, " "), repoPath, err, bytes.TrimSpace(stderr.Bytes()))
	}
	return out, nil
}
