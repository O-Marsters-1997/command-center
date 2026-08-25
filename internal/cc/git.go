package cc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// rebasingBranch names the branch a detached worktree's in-progress rebase lands back on, and
// "" for one detached for any other reason. git calls a rebasing worktree "detached", so keying
// on the branch line alone drops it from every branch-keyed observation (issue #91).
func rebasingBranch(ctx context.Context, worktreePath string) (string, error) {
	for _, dir := range []string{"rebase-merge", "rebase-apply"} {
		out, err := git(ctx, worktreePath, "rev-parse", "--git-path", dir+"/head-name")
		if err != nil {
			return "", err
		}
		headName := strings.TrimSpace(string(out))
		if !filepath.IsAbs(headName) {
			headName = filepath.Join(worktreePath, headName)
		}
		ref, err := os.ReadFile(headName)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read %s for %s: %w", headName, worktreePath, err)
		}
		return strings.TrimPrefix(strings.TrimSpace(string(ref)), "refs/heads/"), nil
	}
	return "", nil
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

// PushRestacked pushes a branch a restack rewrote, leasing on expectedRemote so anything that
// reached origin since the app last recorded a push -- a reviewer's committed suggestion,
// Mergify's own update -- refuses instead of being discarded (issue #89).
func PushRestacked(ctx context.Context, repoPath, branch, expectedRemote string) error {
	lease := "--force-with-lease=" + branch + ":" + expectedRemote
	_, err := git(ctx, repoPath, "push", lease, "origin", branch)
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

// Rebase replays worktreePath's own branch onto onto, dropping every commit reachable from
// upstream. That is the shape a squash-merged base needs: the squash carries the base's work
// under a commit with no ancestry, so replaying the branch's own copies conflicts against it
// (issue #89). A conflict leaves the worktree mid-rebase, which MidMerge reads.
func Rebase(ctx context.Context, worktreePath, onto, upstream string) error {
	_, err := git(ctx, worktreePath, "rebase", "--onto", onto, upstream)
	return err
}

// MergeAbort undoes an unresolved merge or restack in worktreePath, restoring the pre-merge tip
// -- `refresh conflicted`'s only verb (docs/designs/command-centre-design.md § 4a).
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

// MidMerge reports whether worktreePath is left mid-merge or mid-restack, which a resolving
// MERGE_HEAD or REBASE_HEAD means. It is read, never recorded: a human resolving the conflict
// and committing clears it with no bookkeeping (docs/designs/command-centre-design.md § 4a).
func MidMerge(ctx context.Context, worktreePath string) (bool, error) {
	merging, err := refExists(ctx, worktreePath, "MERGE_HEAD")
	if err != nil || merging {
		return merging, err
	}
	return midRebase(ctx, worktreePath)
}

func midRebase(ctx context.Context, worktreePath string) (bool, error) {
	return refExists(ctx, worktreePath, "REBASE_HEAD")
}

// Ancestor reports whether commit is reachable from ref -- false once a squash or a force-push
// has replaced the history commit sat on (issue #89).
func Ancestor(ctx context.Context, repoPath, commit, ref string) (bool, error) {
	return gitSucceeds(ctx, repoPath, "merge-base", "--is-ancestor", commit, ref)
}

func refExists(ctx context.Context, worktreePath, ref string) (bool, error) {
	return gitSucceeds(ctx, worktreePath, "rev-parse", "--verify", "-q", ref)
}

// gitSucceeds runs a git query whose exit status is its answer, so exit 1 is a "no" rather than
// a failure. Every other exit -- 128 for a ref that does not resolve, most of all -- stays an
// error, because a missing commit is not the same answer as a negative one.
func gitSucceeds(ctx context.Context, repoPath string, args ...string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoPath}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("git %s in %s: %w: %s",
			strings.Join(args, " "), repoPath, err, bytes.TrimSpace(stderr.Bytes()))
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
