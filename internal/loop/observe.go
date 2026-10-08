package loop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

// ObserveFunc reads the world. Any non-zero exit ends the tick before anything changes.
type ObserveFunc func(ctx context.Context) (plan.Observation, error)

// NewObserver builds the real observe phase: fetch, then the PR snapshot, then the issue titles,
// then the worktree map, per ready tracked repo. Every branch-keyed map is written under
// plan.BranchKey(repo.Name, branch), since two tracked repos can hold the same branch name.
func NewObserver(store *store.Store, forge gh.Forge, cfg config.Config) ObserveFunc {
	return func(ctx context.Context) (plan.Observation, error) {
		repos, err := readyRepos(ctx, store)
		if err != nil {
			return plan.Observation{}, err
		}
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
			Titles: map[string]string{}, ConflictsWithBase: map[string]bool{},
			ConflictsWithPeer: map[string]map[string]bool{},
			Settings:          map[string]config.RepoSettings{}, SettingsErrors: map[string]string{},
		}
		for _, repo := range repos {
			path := config.CheckoutPath(cfg.DataDir, repo.Name)
			if err := git.Fetch(ctx, path); err != nil {
				return plan.Observation{}, err
			}

			settings, _, settingsErr := config.ReadRepoSettings(ctx, path)
			if settingsErr != nil {
				obs.SettingsErrors[repo.Name] = settingsErr.Error()
			} else {
				obs.Settings[repo.Name] = settings
			}

			branches := branchesFor(tickets, repo.Name)
			snapshot, err := forge.List(ctx, path, branches)
			if err != nil {
				return plan.Observation{}, err
			}
			for branch, pr := range snapshot.ByBranch {
				obs.PRs[plan.BranchKey(repo.Name, branch)] = planPR(pr)
			}
			titles, err := forge.IssueTitles(ctx, path)
			if err != nil {
				return plan.Observation{}, err
			}
			maps.Copy(obs.Titles, titles)

			mainTip, mainErr := git.RevParse(ctx, path, "origin/"+plan.DefaultBaseBranch)
			if mainErr == nil {
				obs.BranchTips[plan.BranchKey(repo.Name, plan.DefaultBaseBranch)] = mainTip
			}
			for _, branch := range branches {
				tip, err := git.RevParse(ctx, path, "origin/"+branch)
				if err != nil {
					continue
				}
				obs.BranchTips[plan.BranchKey(repo.Name, branch)] = tip
				if mainErr != nil {
					continue
				}
				clean, err := git.MergesCleanly(ctx, path, mainTip, tip)
				if err != nil {
					return plan.Observation{}, fmt.Errorf("check whether %s merges into %s: %w",
						branch, plan.DefaultBaseBranch, err)
				}
				obs.ConflictsWithBase[plan.BranchKey(repo.Name, branch)] = !clean
			}

			if err := recordPeerConflicts(
				ctx, path, repo.Name, branches, obs.BranchTips, prevObs, obs.ConflictsWithPeer, git.MergesCleanly,
			); err != nil {
				return plan.Observation{}, err
			}

			worktrees, err := git.WorktreePaths(ctx, path)
			if err != nil {
				return plan.Observation{}, err
			}
			for branch, wtPath := range worktrees {
				obs.Worktrees[plan.BranchKey(repo.Name, branch)] = wtPath
				tip, err := git.BranchTip(ctx, path, branch)
				if err != nil {
					return plan.Observation{}, fmt.Errorf("read local tip of %s: %w", branch, err)
				}
				obs.LocalTips[plan.BranchKey(repo.Name, branch)] = tip
				mid, err := git.MidMerge(ctx, wtPath)
				if err != nil {
					return plan.Observation{}, fmt.Errorf("check mid-merge for %s: %w", branch, err)
				}
				obs.MidMerge[plan.BranchKey(repo.Name, branch)] = mid
			}

			if settings.MergifySHA == "" {
				continue
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

func mergifyHash(ctx context.Context, repoPath string) (string, error) {
	data, err := git.ShowFile(ctx, repoPath, "origin/"+plan.DefaultBaseBranch, ".mergify.yml")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

type peerReader func(ctx context.Context, repoPath, tipA, tipB string) (bool, error)

func recordPeerConflicts(
	ctx context.Context, repoPath, repo string, branches []string, tips map[string]string,
	prev plan.Observation, into map[string]map[string]bool, merges peerReader,
) error {
	for i, branchA := range branches {
		tipA, ok := tips[plan.BranchKey(repo, branchA)]
		if !ok {
			continue
		}
		for _, branchB := range branches[i+1:] {
			tipB, ok := tips[plan.BranchKey(repo, branchB)]
			if !ok {
				continue
			}
			if conflicts, ok := cachedPeerConflict(prev, repo, branchA, tipA, branchB, tipB); ok {
				recordConflictsWithPeer(into, repo, branchA, branchB, conflicts)
				continue
			}
			clean, err := merges(ctx, repoPath, tipA, tipB)
			if err != nil {
				return fmt.Errorf("check whether %s merges with %s: %w", branchA, branchB, err)
			}
			recordConflictsWithPeer(into, repo, branchA, branchB, !clean)
		}
	}
	return nil
}

func cachedPeerConflict(prev plan.Observation, repo, branchA, tipA, branchB, tipB string) (conflicts, ok bool) {
	if prev.BranchTips[plan.BranchKey(repo, branchA)] != tipA || prev.BranchTips[plan.BranchKey(repo, branchB)] != tipB {
		return false, false
	}
	conflicts, ok = prev.ConflictsWithPeer[plan.BranchKey(repo, branchA)][plan.BranchKey(repo, branchB)]
	return conflicts, ok
}

func recordConflictsWithPeer(m map[string]map[string]bool, repo, a, b string, conflicts bool) {
	keyA, keyB := plan.BranchKey(repo, a), plan.BranchKey(repo, b)
	if m[keyA] == nil {
		m[keyA] = map[string]bool{}
	}
	m[keyA][keyB] = conflicts
	if m[keyB] == nil {
		m[keyB] = map[string]bool{}
	}
	m[keyB][keyA] = conflicts
}

func readyRepos(ctx context.Context, st *store.Store) ([]store.Repo, error) {
	repos, err := st.Repos(ctx)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(repos, func(r store.Repo) bool { return r.State != store.RepoReady }), nil
}

func branchesFor(tickets []store.Ticket, repo string) []string {
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

func (l *Loop) rereadLocalTips(ctx context.Context, obs plan.Observation) {
	for key := range obs.Worktrees {
		repo, branch, _ := strings.Cut(key, "//")
		if tip, err := git.BranchTip(ctx, l.checkout(repo), branch); err == nil {
			obs.LocalTips[key] = tip
		}
	}
}
