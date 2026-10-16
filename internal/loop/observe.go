package loop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/git"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

type ObserveFunc func(ctx context.Context) (plan.Observation, error)

func NewObserver(st *store.Store, forge gh.Forge, cfg config.Config) ObserveFunc {
	return func(ctx context.Context) (plan.Observation, error) {
		repos, err := observableRepos(ctx, st, cfg.DataDir)
		if err != nil {
			return plan.Observation{}, fmt.Errorf("list observable repos: %w", err)
		}
		tickets, err := st.Tickets(ctx)
		if err != nil {
			return plan.Observation{}, fmt.Errorf("list tickets: %w", err)
		}
		prevObs, _, err := st.LastObservation(ctx)
		if err != nil {
			return plan.Observation{}, fmt.Errorf("last observation: %w", err)
		}

		obs := plan.NewObservation(time.Time{})
		for _, repo := range repos {
			path := config.CheckoutPath(cfg.DataDir, repo.Name)
			read := repoRead{name: repo.Name, path: path, branches: branchesFor(tickets, repo.Name)}
			part, err := observeRepo(ctx, forge, read, prevObs)
			if err != nil {
				obs.RepoErrors[repo.Name] = err.Error()
				carryForward(&obs, prevObs, repo.Name)
				continue
			}
			mergeObservation(&obs, part)
		}
		return obs, nil
	}
}

func mergeObservation(dst *plan.Observation, src plan.Observation) {
	maps.Copy(dst.PRs, src.PRs)
	maps.Copy(dst.Worktrees, src.Worktrees)
	maps.Copy(dst.MergifyHash, src.MergifyHash)
	maps.Copy(dst.BranchTips, src.BranchTips)
	maps.Copy(dst.LocalTips, src.LocalTips)
	maps.Copy(dst.MidMerge, src.MidMerge)
	maps.Copy(dst.Titles, src.Titles)
	maps.Copy(dst.ConflictsWithBase, src.ConflictsWithBase)
	maps.Copy(dst.ConflictsWithPeer, src.ConflictsWithPeer)
	maps.Copy(dst.Settings, src.Settings)
	maps.Copy(dst.SettingsErrors, src.SettingsErrors)
	maps.Copy(dst.SettingsSources, src.SettingsSources)
}

func carryForward(dst *plan.Observation, prev plan.Observation, repo string) {
	keep := func(key plan.BranchRef) bool { return key.Repo == repo }
	copyKeyed(dst.PRs, prev.PRs, keep)
	copyKeyed(dst.Worktrees, prev.Worktrees, keep)
	copyKeyed(dst.BranchTips, prev.BranchTips, keep)
	copyKeyed(dst.LocalTips, prev.LocalTips, keep)
	copyKeyed(dst.MidMerge, prev.MidMerge, keep)
	copyKeyed(dst.ConflictsWithBase, prev.ConflictsWithBase, keep)
	copyKeyed(dst.ConflictsWithPeer, prev.ConflictsWithPeer, keep)
	copyKeyed(dst.Titles, prev.Titles, func(plan.TicketURL) bool { return true })
	named := func(key string) bool { return key == repo }
	copyKeyed(dst.MergifyHash, prev.MergifyHash, named)
	copyKeyed(dst.Settings, prev.Settings, named)
	copyKeyed(dst.SettingsErrors, prev.SettingsErrors, named)
}

func copyKeyed[K comparable, V any](dst, src map[K]V, keep func(K) bool) {
	for key, value := range src {
		if _, taken := dst[key]; !taken && keep(key) {
			dst[key] = value
		}
	}
}

type repoRead struct {
	name, path string
	branches   []string
}

func observeRepo(
	ctx context.Context, forge gh.Forge, read repoRead, prevObs plan.Observation,
) (plan.Observation, error) {
	name, path, branches := read.name, read.path, read.branches
	obs := plan.NewObservation(time.Time{})
	if err := git.Fetch(ctx, path); err != nil {
		return plan.Observation{}, fmt.Errorf("fetch: %w", err)
	}

	settings, source, settingsErr := config.ReadRepoSettings(ctx, path)
	if settingsErr != nil {
		obs.SettingsErrors[name] = settingsErr.Error()
	} else {
		obs.Settings[name] = settings
		obs.SettingsSources[name] = string(source)
	}

	snapshot, err := forge.List(ctx, path, branches)
	if err != nil {
		return plan.Observation{}, fmt.Errorf("list pull requests: %w", err)
	}
	for branch, pr := range snapshot.ByBranch {
		obs.PRs[plan.BranchRef{Repo: name, Branch: branch}] = planPR(pr)
	}
	titles, err := forge.IssueTitles(ctx, path)
	if err != nil {
		return plan.Observation{}, fmt.Errorf("list issue titles: %w", err)
	}
	for url, title := range titles {
		obs.Titles[plan.TicketURL(url)] = title
	}

	mainTip, mainPresent, err := git.TipIfPresent(ctx, path, "origin/"+plan.DefaultBaseBranch)
	if err != nil {
		return plan.Observation{}, fmt.Errorf("read tip of origin/%s: %w", plan.DefaultBaseBranch, err)
	}
	if mainPresent {
		obs.BranchTips[plan.BranchRef{Repo: name, Branch: plan.DefaultBaseBranch}] = plan.SHA(mainTip)
	}
	for _, branch := range branches {
		tip, present, err := git.TipIfPresent(ctx, path, "origin/"+branch)
		if err != nil {
			return plan.Observation{}, fmt.Errorf("read tip of origin/%s: %w", branch, err)
		}
		if !present {
			continue
		}
		ref := plan.BranchRef{Repo: name, Branch: branch}
		obs.BranchTips[ref] = plan.SHA(tip)
		if !mainPresent {
			continue
		}
		clean, err := git.MergesCleanly(ctx, path, mainTip, tip)
		if err != nil {
			return plan.Observation{}, fmt.Errorf("check whether %s merges into %s: %w",
				branch, plan.DefaultBaseBranch, err)
		}
		obs.ConflictsWithBase[ref] = !clean
	}

	scan := peerScan{
		repoPath: path, repo: name, branches: branches, tips: obs.BranchTips,
		prev: prevObs, into: obs.ConflictsWithPeer, merges: git.MergesCleanly,
	}
	if err := recordPeerConflicts(ctx, scan); err != nil {
		return plan.Observation{}, fmt.Errorf("record peer conflicts: %w", err)
	}

	worktrees, err := git.WorktreePaths(ctx, path)
	if err != nil {
		return plan.Observation{}, fmt.Errorf("list worktrees: %w", err)
	}
	for branch, wtPath := range worktrees {
		ref := plan.BranchRef{Repo: name, Branch: branch}
		obs.Worktrees[ref] = wtPath
		tip, err := git.BranchTip(ctx, path, branch)
		if err != nil {
			return plan.Observation{}, fmt.Errorf("read local tip of %s: %w", branch, err)
		}
		obs.LocalTips[ref] = plan.SHA(tip)
		mid, err := git.MidMerge(ctx, wtPath)
		if err != nil {
			return plan.Observation{}, fmt.Errorf("check mid-merge for %s: %w", branch, err)
		}
		obs.MidMerge[ref] = mid
	}

	if settings.MergifySHA != "" {
		hash, err := mergifyHash(ctx, path)
		if err != nil {
			return plan.Observation{}, fmt.Errorf("hash .mergify.yml for %s: %w", name, err)
		}
		obs.MergifyHash[name] = hash
	}
	return obs, nil
}

func mergifyHash(ctx context.Context, repoPath string) (string, error) {
	data, err := git.ShowFile(ctx, repoPath, "origin/"+plan.DefaultBaseBranch, ".mergify.yml")
	if err != nil {
		return "", fmt.Errorf("show .mergify.yml: %w", err)
	}
	sum := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

type peerReader func(ctx context.Context, repoPath, tipA, tipB string) (bool, error)

type peerScan struct {
	repoPath, repo string
	branches       []string
	tips           map[plan.BranchRef]plan.SHA
	prev           plan.Observation
	into           map[plan.BranchRef]map[plan.BranchRef]bool
	merges         peerReader
}

type peerBranch struct {
	ref plan.BranchRef
	tip plan.SHA
}

type peerPair struct{ a, b peerBranch }

func recordPeerConflicts(ctx context.Context, scan peerScan) error {
	for i, branchA := range scan.branches {
		refA := plan.BranchRef{Repo: scan.repo, Branch: branchA}
		tipA, ok := scan.tips[refA]
		if !ok {
			continue
		}
		for _, branchB := range scan.branches[i+1:] {
			refB := plan.BranchRef{Repo: scan.repo, Branch: branchB}
			tipB, ok := scan.tips[refB]
			if !ok {
				continue
			}
			pair := peerPair{a: peerBranch{ref: refA, tip: tipA}, b: peerBranch{ref: refB, tip: tipB}}
			if conflicts, ok := cachedPeerConflict(scan.prev, pair); ok {
				recordConflictsWithPeer(scan.into, pair, conflicts)
				continue
			}
			clean, err := scan.merges(ctx, scan.repoPath, string(tipA), string(tipB))
			if err != nil {
				return fmt.Errorf("check whether %s merges with %s: %w", branchA, branchB, err)
			}
			recordConflictsWithPeer(scan.into, pair, !clean)
		}
	}
	return nil
}

func cachedPeerConflict(prev plan.Observation, pair peerPair) (conflicts, ok bool) {
	if prev.BranchTips[pair.a.ref] != pair.a.tip || prev.BranchTips[pair.b.ref] != pair.b.tip {
		return false, false
	}
	conflicts, ok = prev.ConflictsWithPeer[pair.a.ref][pair.b.ref]
	return conflicts, ok
}

func recordConflictsWithPeer(m map[plan.BranchRef]map[plan.BranchRef]bool, pair peerPair, conflicts bool) {
	keyA, keyB := pair.a.ref, pair.b.ref
	if m[keyA] == nil {
		m[keyA] = map[plan.BranchRef]bool{}
	}
	m[keyA][keyB] = conflicts
	if m[keyB] == nil {
		m[keyB] = map[plan.BranchRef]bool{}
	}
	m[keyB][keyA] = conflicts
}

func observableRepos(ctx context.Context, st *store.Store, dataDir string) ([]store.Repo, error) {
	repos, err := st.Repos(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	return slices.DeleteFunc(repos, func(r store.Repo) bool {
		if r.Phase == plan.RepoCloning {
			return true
		}
		_, err := os.Stat(config.CheckoutPath(dataDir, r.Name))
		return err != nil
	}), nil
}

func readyRepos(ctx context.Context, st *store.Store) ([]store.Repo, error) {
	repos, err := st.Repos(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	return slices.DeleteFunc(repos, func(r store.Repo) bool { return r.Phase != plan.RepoReady }), nil
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
		Number: pr.Number, HeadRef: pr.HeadRef, HeadOid: plan.SHA(pr.HeadOid), BaseRef: pr.BaseRef,
		BaseOid:     plan.SHA(pr.BaseOid),
		AuthorLogin: pr.AuthorLogin, IsDraft: pr.IsDraft, State: plan.PRState(pr.State),
		Checks: checks, Labels: pr.Labels, MergedAt: pr.MergedAt,
	}
}

func (l *Loop) rereadLocalTips(ctx context.Context, obs plan.Observation) {
	for key := range obs.Worktrees {
		if tip, err := git.BranchTip(ctx, l.checkout(key.Repo), key.Branch); err == nil {
			obs.LocalTips[key] = plan.SHA(tip)
		}
	}
}
