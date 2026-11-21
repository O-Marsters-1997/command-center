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

	"github.com/O-Marsters-1997/command-center/internal/adapter/gh"
	"github.com/O-Marsters-1997/command-center/internal/adapter/git"
	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/plan"
	"github.com/O-Marsters-1997/command-center/internal/plan/verdict"
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
		pushedTips, err := st.LastPushedTips(ctx)
		if err != nil {
			return plan.Observation{}, fmt.Errorf("last pushed tips: %w", err)
		}

		obs := plan.NewObservation(time.Time{})
		for _, repo := range repos {
			path := config.CheckoutPath(cfg.DataDir, repo.Name)
			read := repoRead{
				name: repo.Name, path: path, branches: branchesFor(tickets, repo.Name),
				pushed: pushedTipsFor(tickets, repo.Name, pushedTips),
			}
			part, err := observeRepo(ctx, forge, read, prevObs)
			if err != nil {
				obs.RepoErrors[repo.Name] = err.Error()
				obs.CarryForward(prevObs, repo.Name)
				continue
			}
			obs.Merge(part)
		}
		return obs, nil
	}
}

type repoRead struct {
	name, path string
	branches   []string
	pushed     map[string]plan.SHA
}

func pushedTipsFor(tickets []store.Ticket, repo string, tips map[plan.TicketURL]plan.SHA) map[string]plan.SHA {
	out := map[string]plan.SHA{}
	for _, t := range tickets {
		if tip, ok := tips[plan.TicketURL(t.URL)]; ok && t.Repo == repo {
			out[t.Branch] = tip
		}
	}
	return out
}

func observeRepo(
	ctx context.Context, forge gh.Forge, read repoRead, prevObs plan.Observation,
) (plan.Observation, error) {
	obs := plan.NewObservation(time.Time{})
	if err := git.Fetch(ctx, read.path); err != nil {
		return plan.Observation{}, fmt.Errorf("fetch: %w", err)
	}

	settings, source, settingsErr := readRepoSettings(ctx, read.path)
	if settingsErr != nil {
		obs.SettingsErrors[read.name] = settingsErr.Error()
	} else {
		obs.Settings[read.name] = settings
		obs.SettingsSources[read.name] = string(source)
	}

	if err := observePullRequests(ctx, forge, read, &obs); err != nil {
		return plan.Observation{}, err
	}
	if err := observeTips(ctx, read, prevObs, &obs); err != nil {
		return plan.Observation{}, err
	}
	if err := observeWorktrees(ctx, read, &obs); err != nil {
		return plan.Observation{}, err
	}

	if settings.MergifySHA != "" {
		hash, err := mergifyHash(ctx, read.path)
		if err != nil {
			return plan.Observation{}, fmt.Errorf("hash .mergify.yml for %s: %w", read.name, err)
		}
		obs.MergifyHash[read.name] = hash
	}
	return obs, nil
}

func observePullRequests(ctx context.Context, forge gh.Forge, read repoRead, obs *plan.Observation) error {
	snapshot, err := forge.List(ctx, read.path, read.branches)
	if err != nil {
		return fmt.Errorf("list pull requests: %w", err)
	}
	for branch, pr := range snapshot.ByBranch {
		if pr.State == gh.Open {
			if pr.Checks, err = withAttempts(ctx, forge, read.path, pr.Checks); err != nil {
				return fmt.Errorf("read check attempts for %s: %w", branch, err)
			}
		}
		ref := plan.BranchRef{Repo: read.name, Branch: branch}
		obs.PRs[ref] = planPR(pr)
		pushed := read.pushed[branch]
		if pr.State != gh.Open || pushed == "" || pr.HeadOid == "" || plan.SHA(pr.HeadOid) == pushed {
			continue
		}
		descends, err := git.AncestorIfPresent(ctx, read.path, string(pushed), pr.HeadOid)
		if err != nil {
			return fmt.Errorf("check %s descends from %s: %w", branch, pushed, err)
		}
		obs.HeadDescends[ref] = descends
	}
	titles, err := forge.IssueTitles(ctx, read.path)
	if err != nil {
		return fmt.Errorf("list issue titles: %w", err)
	}
	for url, title := range titles {
		obs.Titles[plan.TicketURL(url)] = title
	}
	return nil
}

func observeTips(ctx context.Context, read repoRead, prevObs plan.Observation, obs *plan.Observation) error {
	mainTip, mainPresent, err := git.TipIfPresent(ctx, read.path, "origin/"+plan.DefaultBaseBranch)
	if err != nil {
		return fmt.Errorf("read tip of origin/%s: %w", plan.DefaultBaseBranch, err)
	}
	if mainPresent {
		obs.BranchTips[plan.BranchRef{Repo: read.name, Branch: plan.DefaultBaseBranch}] = plan.SHA(mainTip)
	}
	for _, branch := range read.branches {
		tip, present, err := git.TipIfPresent(ctx, read.path, "origin/"+branch)
		if err != nil {
			return fmt.Errorf("read tip of origin/%s: %w", branch, err)
		}
		if !present {
			continue
		}
		ref := plan.BranchRef{Repo: read.name, Branch: branch}
		obs.BranchTips[ref] = plan.SHA(tip)
		if !mainPresent {
			continue
		}
		clean, err := git.MergesCleanly(ctx, read.path, mainTip, tip)
		if err != nil {
			return fmt.Errorf("check whether %s merges into %s: %w", branch, plan.DefaultBaseBranch, err)
		}
		obs.ConflictsWithBase[ref] = !clean
	}

	scan := peerScan{
		repoPath: read.path, repo: read.name, branches: read.branches, tips: obs.BranchTips,
		prev: prevObs, into: obs.ConflictsWithPeer, merges: git.MergesCleanly,
	}
	if err := recordPeerConflicts(ctx, scan); err != nil {
		return fmt.Errorf("record peer conflicts: %w", err)
	}
	return nil
}

func observeWorktrees(ctx context.Context, read repoRead, obs *plan.Observation) error {
	worktrees, err := git.WorktreePaths(ctx, read.path)
	if err != nil {
		return fmt.Errorf("list worktrees: %w", err)
	}
	for branch, wtPath := range worktrees {
		ref := plan.BranchRef{Repo: read.name, Branch: branch}
		obs.Worktrees[ref] = wtPath
		tip, err := git.BranchTip(ctx, read.path, branch)
		if err != nil {
			return fmt.Errorf("read local tip of %s: %w", branch, err)
		}
		obs.LocalTips[ref] = plan.SHA(tip)
		mid, err := git.MidMerge(ctx, wtPath)
		if err != nil {
			return fmt.Errorf("check mid-merge for %s: %w", branch, err)
		}
		obs.MidMerge[ref] = mid
	}
	return nil
}

func withAttempts(
	ctx context.Context, forge gh.Forge, path string, checks map[string]gh.CheckState,
) (map[string]gh.CheckState, error) {
	out := maps.Clone(checks)
	byRun := map[string]int{}
	for name, c := range checks {
		if plan.ToVerdictCheckState(plan.CheckState(c)) != verdict.Failure {
			continue
		}
		id, err := gh.RunID(c.DetailsURL)
		if err != nil {
			continue
		}
		attempt, read := byRun[id]
		if !read {
			if attempt, err = forge.Attempt(ctx, path, c.DetailsURL); err != nil {
				return nil, fmt.Errorf("check %s: %w", name, err)
			}
			byRun[id] = attempt
		}
		c.Attempt = attempt
		out[name] = c
	}
	return out, nil
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
		Checks: checks, Labels: pr.Labels, MergedAt: pr.MergedAt, Title: pr.Title, URL: pr.URL,
	}
}

func (l *Loop) rereadLocalTips(ctx context.Context, obs plan.Observation) {
	for key := range obs.Worktrees {
		tip, err := git.BranchTip(ctx, l.checkout(key.Repo), key.Branch)
		if err != nil {
			l.logger.Warn("reread local tip", "repo", key.Repo, "branch", key.Branch, "err", err)
			continue
		}
		obs.LocalTips[key] = plan.SHA(tip)
	}
}
