package cc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/gh"
)

// Observation is everything one tick read from the world. It is persisted so that the page
// can render after a restart, and so that a failed tick shows the last good facts rather than
// an empty table. It holds facts, never state labels — those stay derived (inv. 14).
type Observation struct {
	ObservedAt time.Time                 `json:"observed_at"`
	PRs        map[string]gh.PR          `json:"prs"`
	Worktrees  map[string]string         `json:"worktrees"`
	Runs       map[string]RunObservation `json:"runs"`
	// MergifyHash is each configured repo's current sha256(.mergify.yml), keyed by repo name --
	// only for a repo that names a mergify_sha to compare against (§7's staleness detector).
	// A repo with no predicate configured is never read, so an untracked repo's missing file
	// never fails a tick.
	MergifyHash map[string]string `json:"mergify_hash"`
	// BranchTips is git rev-parse origin/<branch>, post-fetch, keyed by branch (§4a): the git
	// fact a stacked base's tip is compared against, rather than the PR snapshot's headRefOid --
	// one indirection off it, and stale the moment a reviewer pushes without GitHub re-reporting.
	BranchTips map[string]string `json:"branch_tips"`
	// MidMerge reports whether each branch's own worktree is left mid-merge, read fresh every
	// tick (§4a) -- never recorded, since a human resolving the conflict by hand and committing
	// must clear it with no bookkeeping.
	MidMerge map[string]bool `json:"mid_merge"`
}

// RunObservation is one task's liveness as read this tick, keyed by task_id. Persisting it
// is what lets the page render pgid/elapsed/log path after a restart without a tick
// re-probing between requests.
type RunObservation struct {
	Alive bool `json:"alive"`
}

// ObserveFunc reads the world. Any non-zero exit ends the tick before anything changes.
type ObserveFunc func(ctx context.Context) (Observation, error)

// NewObserver builds the real observe phase: fetch, then the PR snapshot, then the worktree
// map, per configured repo. Branches are keyed globally, not per repo — a same-named branch
// in two repos would collide; key by (repo, branch) when Phase 2 adds a second repo.
func NewObserver(store *Store, cfg Config, root string) ObserveFunc {
	return func(ctx context.Context) (Observation, error) {
		tasks, err := store.Tasks(ctx)
		if err != nil {
			return Observation{}, err
		}

		obs := Observation{
			PRs: map[string]gh.PR{}, Worktrees: map[string]string{}, MergifyHash: map[string]string{},
			BranchTips: map[string]string{}, MidMerge: map[string]bool{},
		}
		for _, repo := range cfg.Repos {
			path := filepath.Join(root, repo.Path)
			if err := Fetch(ctx, path); err != nil {
				return Observation{}, err
			}

			branches := branchesFor(tasks, repo.Name)
			snapshot, err := gh.List(ctx, path, branches)
			if err != nil {
				return Observation{}, err
			}
			for branch, pr := range snapshot.ByBranch {
				obs.PRs[branch] = pr
			}
			for _, branch := range branches {
				if tip, err := RevParse(ctx, path, "origin/"+branch); err == nil {
					obs.BranchTips[branch] = tip
				}
			}
			// defaultBaseBranch's own tip is read too (§4a), under mainTipKey rather than its plain
			// name: unlike a task's own branch, every repo has a "main", so the plain name would
			// collide the moment a second repo is configured.
			if tip, err := RevParse(ctx, path, "origin/"+defaultBaseBranch); err == nil {
				obs.BranchTips[mainTipKey(repo.Name)] = tip
			}

			worktrees, err := Worktrees(ctx, path)
			if err != nil {
				return Observation{}, err
			}
			for branch, wtPath := range worktrees {
				obs.Worktrees[branch] = wtPath
				mid, err := MidMerge(ctx, wtPath)
				if err != nil {
					return Observation{}, fmt.Errorf("check mid-merge for %s: %w", branch, err)
				}
				obs.MidMerge[branch] = mid
			}

			if repo.MergifySHA == "" {
				continue // no predicate opted in; nothing to hash or gate on (§7)
			}
			hash, err := mergifyHash(path)
			if err != nil {
				return Observation{}, fmt.Errorf("hash .mergify.yml for %s: %w", repo.Name, err)
			}
			obs.MergifyHash[repo.Name] = hash
		}
		return obs, nil
	}
}

// mergifyHash reads and hashes repoPath's .mergify.yml, formatted to match the mergify_sha a
// human records in config after reviewing the file (docs/designs/command-centre-design.md § 7's example,
// "sha256:…").
func mergifyHash(repoPath string) (string, error) {
	data, err := os.ReadFile(filepath.Join(repoPath, ".mergify.yml"))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func branchesFor(tasks []Task, repo string) []string {
	var branches []string
	for _, t := range tasks {
		if t.Repo == repo {
			branches = append(branches, t.Branch)
		}
	}
	return branches
}
