package cc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
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
	// Titles is each configured repo's open issue titles, keyed by issue URL, which is what a
	// ticket's own url holds. `gh issue list`'s 100-row limit leaves a ticket absent rather
	// than mis-keyed.
	Titles map[string]string `json:"titles"`
	// MidMerge reports whether each branch's own worktree is left mid-merge, read fresh every
	// tick (§4a) -- never recorded, since a human resolving the conflict by hand and committing
	// must clear it with no bookkeeping.
	MidMerge map[string]bool `json:"mid_merge"`
	// ConflictsWithBase reports whether origin/<branch> would conflict with origin/main, keyed
	// by branch. It is what the launch gate refuses on: a child cut from a base that already
	// conflicts inherits the conflict (docs/adr/0006-resolve-a-conflict-once.md).
	ConflictsWithBase map[string]bool `json:"conflicts_with_base"`
	// ConflictedPaths names each conflicting branch's own conflicted paths, keyed by branch.
	ConflictedPaths map[string][]string `json:"conflicted_paths"`
	// ConflictsWithPeer reports whether two branches' tips would conflict if merged together,
	// keyed by each branch under the other. Observe only ever records the pair, never which one
	// yields: it has no ref order to decide that (docs/adr/0010-one-conflicting-peer-at-a-time.md).
	ConflictsWithPeer map[string]map[string]bool `json:"conflicts_with_peer"`
}

// RunObservation is one ticket's liveness as read this tick, keyed by ticket_id. Persisting it
// is what lets the page render pgid/elapsed/log path after a restart without a tick
// re-probing between requests.
type RunObservation struct {
	Alive bool `json:"alive"`
}

// ObserveFunc reads the world. Any non-zero exit ends the tick before anything changes.
type ObserveFunc func(ctx context.Context) (Observation, error)

// NewObserver builds the real observe phase: fetch, then the PR snapshot, then the issue titles,
// then the worktree map, per configured repo. Branches are keyed globally, not per repo — a
// same-named branch in two repos would collide; key by (repo, branch) when Phase 2 adds a second repo.
func NewObserver(store *Store, cfg Config) ObserveFunc {
	return func(ctx context.Context) (Observation, error) {
		tickets, err := store.Tickets(ctx)
		if err != nil {
			return Observation{}, err
		}
		prevObs, _, err := store.LastObservation(ctx)
		if err != nil {
			return Observation{}, err
		}

		obs := Observation{
			PRs: map[string]gh.PR{}, Worktrees: map[string]string{}, MergifyHash: map[string]string{},
			BranchTips: map[string]string{}, MidMerge: map[string]bool{}, Titles: map[string]string{},
			ConflictsWithBase: map[string]bool{}, ConflictedPaths: map[string][]string{},
			ConflictsWithPeer: map[string]map[string]bool{},
		}
		for _, repo := range cfg.Repos {
			path := repo.Checkout
			if err := Fetch(ctx, path); err != nil {
				return Observation{}, err
			}

			branches := branchesFor(tickets, repo.Name)
			snapshot, err := gh.List(ctx, path, branches)
			if err != nil {
				return Observation{}, err
			}
			for branch, pr := range snapshot.ByBranch {
				obs.PRs[branch] = pr
			}
			titles, err := gh.IssueTitles(ctx, path)
			if err != nil {
				return Observation{}, err
			}
			maps.Copy(obs.Titles, titles)

			// defaultBaseBranch's own tip is read too (§4a), under mainTipKey rather than its plain
			// name: unlike a ticket's own branch, every repo has a "main", so the plain name would
			// collide the moment a second repo is configured.
			mainTip, mainErr := RevParse(ctx, path, "origin/"+defaultBaseBranch)
			if mainErr == nil {
				obs.BranchTips[mainTipKey(repo.Name)] = mainTip
			}
			for _, branch := range branches {
				tip, err := RevParse(ctx, path, "origin/"+branch)
				if err != nil {
					continue // never pushed, so there is no remote branch to read or to cut from
				}
				obs.BranchTips[branch] = tip
				if mainErr != nil {
					continue
				}
				clean, paths, err := MergesCleanly(ctx, path, mainTip, tip)
				if err != nil {
					return Observation{}, fmt.Errorf("check whether %s merges into %s: %w",
						branch, defaultBaseBranch, err)
				}
				obs.ConflictsWithBase[branch] = !clean
				if !clean {
					obs.ConflictedPaths[branch] = paths
				}
			}

			if err := recordPeerConflicts(
				ctx, path, branches, obs.BranchTips, prevObs, obs.ConflictsWithPeer, MergesCleanly,
			); err != nil {
				return Observation{}, err
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
			hash, err := mergifyHash(ctx, path)
			if err != nil {
				return Observation{}, fmt.Errorf("hash .mergify.yml for %s: %w", repo.Name, err)
			}
			obs.MergifyHash[repo.Name] = hash
		}
		return obs, nil
	}
}

// mergifyHash hashes .mergify.yml as origin's default branch holds it, formatted to match the
// mergify_sha a human records after reviewing the file (docs/designs/command-centre-design.md
// § 7). The ref, not the working tree: a dirty checkout is not a config change.
func mergifyHash(ctx context.Context, repoPath string) (string, error) {
	data, err := ShowFile(ctx, repoPath, "origin/"+defaultBaseBranch, ".mergify.yml")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// peerReader is MergesCleanly's shape, the seam a test replaces to count calls instead of
// shelling out to git.
type peerReader func(ctx context.Context, repoPath, tipA, tipB string) (bool, []string, error)

// recordPeerConflicts fills in ConflictsWithPeer for one repo's branches, reusing the prior
// tick's read for any pair whose two tips have not moved since (#180,
// docs/adr/0010-one-conflicting-peer-at-a-time.md): a pair's answer only changes when one of
// its two tips moves, and BranchTips already carries them, so an unmoved pair costs no
// merge-tree call at all. A zero-value prev (nothing observed yet) never matches a real tip,
// so a cold start falls through to merges for every pair without special-casing it.
func recordPeerConflicts(
	ctx context.Context, repoPath string, branches []string, tips map[string]string,
	prev Observation, into map[string]map[string]bool, merges peerReader,
) error {
	for i, branchA := range branches {
		tipA, ok := tips[branchA]
		if !ok {
			continue
		}
		for _, branchB := range branches[i+1:] {
			tipB, ok := tips[branchB]
			if !ok {
				continue
			}
			if conflicts, ok := cachedPeerConflict(prev, branchA, tipA, branchB, tipB); ok {
				recordConflictsWithPeer(into, branchA, branchB, conflicts)
				continue
			}
			clean, _, err := merges(ctx, repoPath, tipA, tipB)
			if err != nil {
				return fmt.Errorf("check whether %s merges with %s: %w", branchA, branchB, err)
			}
			recordConflictsWithPeer(into, branchA, branchB, !clean)
		}
	}
	return nil
}

// cachedPeerConflict returns the prior tick's read for (branchA, branchB), valid only when both
// tips still match what that tick observed.
func cachedPeerConflict(prev Observation, branchA, tipA, branchB, tipB string) (conflicts, ok bool) {
	if prev.BranchTips[branchA] != tipA || prev.BranchTips[branchB] != tipB {
		return false, false
	}
	conflicts, ok = prev.ConflictsWithPeer[branchA][branchB]
	return conflicts, ok
}

// recordConflictsWithPeer stores one pair's result under both branch names, so a later lookup
// works from either side once ref order (internal/cc's decide step) says which one is "this"
// branch and which the peer.
func recordConflictsWithPeer(m map[string]map[string]bool, a, b string, conflicts bool) {
	if m[a] == nil {
		m[a] = map[string]bool{}
	}
	m[a][b] = conflicts
	if m[b] == nil {
		m[b] = map[string]bool{}
	}
	m[b][a] = conflicts
}

func branchesFor(tickets []Ticket, repo string) []string {
	var branches []string
	for _, t := range tickets {
		if t.Repo == repo {
			branches = append(branches, t.Branch)
		}
	}
	return branches
}
