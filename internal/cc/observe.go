package cc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// ObserveFunc reads the world. Any non-zero exit ends the tick before anything changes.
type ObserveFunc func(ctx context.Context) (plan.Observation, error)

// NewObserver builds the real observe phase: fetch, then the PR snapshot, then the issue titles,
// then the worktree map, per configured repo. Every branch-keyed map is written under
// branchKey(repo.Name, branch), since two configured repos can hold the same branch name.
func NewObserver(store *Store, forge gh.Forge, cfg Config) ObserveFunc {
	return func(ctx context.Context) (plan.Observation, error) {
		tickets, err := store.Tickets(ctx)
		if err != nil {
			return plan.Observation{}, err
		}
		prevObs, _, err := store.LastObservation(ctx)
		if err != nil {
			return plan.Observation{}, err
		}

		obs := plan.Observation{
			PRs: map[string]plan.PR{}, Worktrees: map[string]string{}, MergifyHash: map[string]string{},
			BranchTips: map[string]string{}, LocalTips: map[string]string{}, MidMerge: map[string]bool{},
			Titles: map[string]string{}, ConflictsWithBase: map[string]bool{}, ConflictedPaths: map[string][]string{},
			ConflictsWithPeer: map[string]map[string]bool{},
		}
		for _, repo := range cfg.Repos {
			path := repo.Checkout
			if err := Fetch(ctx, path); err != nil {
				return plan.Observation{}, err
			}

			branches := branchesFor(tickets, repo.Name)
			snapshot, err := forge.List(ctx, path, branches)
			if err != nil {
				return plan.Observation{}, err
			}
			for branch, pr := range snapshot.ByBranch {
				obs.PRs[branchKey(repo.Name, branch)] = planPR(pr)
			}
			titles, err := forge.IssueTitles(ctx, path)
			if err != nil {
				return plan.Observation{}, err
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
				obs.BranchTips[branchKey(repo.Name, branch)] = tip
				if mainErr != nil {
					continue
				}
				clean, paths, err := MergesCleanly(ctx, path, mainTip, tip)
				if err != nil {
					return plan.Observation{}, fmt.Errorf("check whether %s merges into %s: %w",
						branch, defaultBaseBranch, err)
				}
				obs.ConflictsWithBase[branchKey(repo.Name, branch)] = !clean
				if !clean {
					obs.ConflictedPaths[branchKey(repo.Name, branch)] = paths
				}
			}

			if err := recordPeerConflicts(
				ctx, path, repo.Name, branches, obs.BranchTips, prevObs, obs.ConflictsWithPeer, MergesCleanly,
			); err != nil {
				return plan.Observation{}, err
			}

			worktrees, err := Worktrees(ctx, path)
			if err != nil {
				return plan.Observation{}, err
			}
			for branch, wtPath := range worktrees {
				obs.Worktrees[branchKey(repo.Name, branch)] = wtPath
				tip, err := BranchTip(ctx, path, branch)
				if err != nil {
					return plan.Observation{}, fmt.Errorf("read local tip of %s: %w", branch, err)
				}
				obs.LocalTips[branchKey(repo.Name, branch)] = tip
				mid, err := MidMerge(ctx, wtPath)
				if err != nil {
					return plan.Observation{}, fmt.Errorf("check mid-merge for %s: %w", branch, err)
				}
				obs.MidMerge[branchKey(repo.Name, branch)] = mid
			}

			if repo.MergifySHA == "" {
				continue // no predicate opted in; nothing to hash or gate on (§7)
			}
			hash, err := mergifyHash(ctx, path)
			if err != nil {
				return plan.Observation{}, fmt.Errorf("hash .mergify.yml for %s: %w", repo.Name, err)
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
// docs/adr/0004-conflicts-resolve-once-and-one-peer-at-a-time.md): a pair's answer only changes when one of
// its two tips moves, and BranchTips already carries them, so an unmoved pair costs no
// merge-tree call at all. A zero-value prev (nothing observed yet) never matches a real tip,
// so a cold start falls through to merges for every pair without special-casing it.
func recordPeerConflicts(
	ctx context.Context, repoPath, repo string, branches []string, tips map[string]string,
	prev plan.Observation, into map[string]map[string]bool, merges peerReader,
) error {
	for i, branchA := range branches {
		tipA, ok := tips[branchKey(repo, branchA)]
		if !ok {
			continue
		}
		for _, branchB := range branches[i+1:] {
			tipB, ok := tips[branchKey(repo, branchB)]
			if !ok {
				continue
			}
			if conflicts, ok := cachedPeerConflict(prev, repo, branchA, tipA, branchB, tipB); ok {
				recordConflictsWithPeer(into, repo, branchA, branchB, conflicts)
				continue
			}
			clean, _, err := merges(ctx, repoPath, tipA, tipB)
			if err != nil {
				return fmt.Errorf("check whether %s merges with %s: %w", branchA, branchB, err)
			}
			recordConflictsWithPeer(into, repo, branchA, branchB, !clean)
		}
	}
	return nil
}

// cachedPeerConflict returns the prior tick's read for (branchA, branchB), valid only when both
// tips still match what that tick observed.
func cachedPeerConflict(prev plan.Observation, repo, branchA, tipA, branchB, tipB string) (conflicts, ok bool) {
	if prev.BranchTips[branchKey(repo, branchA)] != tipA || prev.BranchTips[branchKey(repo, branchB)] != tipB {
		return false, false
	}
	conflicts, ok = prev.ConflictsWithPeer[branchKey(repo, branchA)][branchKey(repo, branchB)]
	return conflicts, ok
}

// recordConflictsWithPeer stores one pair's result under both branch names, so a later lookup
// works from either side once ref order (internal/cc's decide step) says which one is "this"
// branch and which the peer.
func recordConflictsWithPeer(m map[string]map[string]bool, repo, a, b string, conflicts bool) {
	keyA, keyB := branchKey(repo, a), branchKey(repo, b)
	if m[keyA] == nil {
		m[keyA] = map[string]bool{}
	}
	m[keyA][keyB] = conflicts
	if m[keyB] == nil {
		m[keyB] = map[string]bool{}
	}
	m[keyB][keyA] = conflicts
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

func planPR(pr gh.PR) plan.PR {
	var checks map[string]plan.CheckState
	if pr.Checks != nil {
		checks = make(map[string]plan.CheckState, len(pr.Checks))
		for name, c := range pr.Checks {
			checks[name] = plan.CheckState(c)
		}
	}
	return plan.PR{
		Number: pr.Number, HeadRef: pr.HeadRef, HeadOid: pr.HeadOid, BaseRef: pr.BaseRef, BaseOid: pr.BaseOid,
		AuthorLogin: pr.AuthorLogin, IsDraft: pr.IsDraft, State: plan.PRState(pr.State),
		Checks: checks, Labels: pr.Labels, MergedAt: pr.MergedAt,
	}
}

func rereadLocalTips(ctx context.Context, obs plan.Observation, repoPaths map[string]string) {
	for key := range obs.Worktrees {
		repo, branch, _ := strings.Cut(key, "//")
		if tip, err := BranchTip(ctx, repoPaths[repo], branch); err == nil {
			obs.LocalTips[key] = tip
		}
	}
}
