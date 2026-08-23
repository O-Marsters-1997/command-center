package cc

import (
	"context"
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
}

// RunObservation is one task's liveness as read this tick, keyed by task_id. Persisting it on
// the Observation is what lets the page render pgid/elapsed/log path after a restart without
// re-probing between requests: LatestRunsByTask has the facts, this has the one thing only a
// tick's own liveness check can answer.
type RunObservation struct {
	Alive bool `json:"alive"`
}

// ObserveFunc reads the world. Any non-zero exit ends the tick before anything changes.
type ObserveFunc func(ctx context.Context) (Observation, error)

// NewObserver builds the real observe phase: fetch, then the PR snapshot, then the worktree
// map, per configured repo.
//
// Branches are keyed globally, not per repo: Phase 1 runs one repo, and the same branch name
// in two repos would collide. Key by (repo, branch) when Phase 2 adds the second repo.
func NewObserver(store *Store, cfg Config, root string) ObserveFunc {
	return func(ctx context.Context) (Observation, error) {
		tasks, err := store.Tasks(ctx)
		if err != nil {
			return Observation{}, err
		}

		obs := Observation{PRs: map[string]gh.PR{}, Worktrees: map[string]string{}}
		for _, repo := range cfg.Repos {
			path := filepath.Join(root, repo.Path)
			if err := Fetch(ctx, path); err != nil {
				return Observation{}, err
			}

			snapshot, err := gh.List(ctx, path, branchesFor(tasks, repo.Name))
			if err != nil {
				return Observation{}, err
			}
			for branch, pr := range snapshot.ByBranch {
				obs.PRs[branch] = pr
			}

			worktrees, err := Worktrees(ctx, path)
			if err != nil {
				return Observation{}, err
			}
			for branch, wtPath := range worktrees {
				obs.Worktrees[branch] = wtPath
			}
		}
		return obs, nil
	}
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
