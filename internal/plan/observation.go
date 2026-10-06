package plan

import "time"

// CheckState is one gating check, after the forge's rollup has been collapsed to the latest
// completed run per check name.
type CheckState struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	DetailsURL string    `json:"details_url"`
	StartedAt  time.Time `json:"started_at"`
}

// PR is one pull request as the tick observed it. The JSON tags are the persisted observation's
// shape, so a row saved before PR moved here still loads.
type PR struct {
	Number      int                   `json:"number"`
	HeadRef     string                `json:"head_ref"`
	HeadOid     string                `json:"head_oid"`
	BaseRef     string                `json:"base_ref"`
	BaseOid     string                `json:"base_oid"`
	AuthorLogin string                `json:"author_login"`
	IsDraft     bool                  `json:"is_draft"`
	State       PRState               `json:"state"`
	Checks      map[string]CheckState `json:"checks"`
	Labels      []string              `json:"labels"`
	MergedAt    time.Time             `json:"merged_at"`
}

// RunObservation is one ticket's liveness as read this tick, keyed by ticket_id.
type RunObservation struct {
	Alive bool `json:"alive"`
}

// Observation is everything one tick read from the world. It is persisted so the page can
// render after a restart, and so a failed tick shows the last good facts. It holds facts, never
// state labels. Branch-keyed maps are keyed repo + "//" + branch, since two configured repos can
// hold the same branch name.
type Observation struct {
	ObservedAt time.Time                 `json:"observed_at"`
	PRs        map[string]PR             `json:"prs"`
	Worktrees  map[string]string         `json:"worktrees"`
	Runs       map[string]RunObservation `json:"runs"`
	// MergifyHash is each configured repo's current sha256(.mergify.yml), keyed by repo name, for
	// a repo that names a mergify_sha to compare against only.
	MergifyHash map[string]string `json:"mergify_hash"`
	// BranchTips is git rev-parse origin/<branch>, post-fetch, the git fact a stacked base's tip
	// is compared against.
	BranchTips map[string]string `json:"branch_tips"`
	// LocalTips is each worktree branch's own local tip (refs/heads/<branch>), which is what a
	// push compares against the last pushed tip. A row saved before this field leaves it nil.
	LocalTips map[string]string `json:"local_tips"`
	// Titles is each configured repo's open issue titles, keyed by issue URL.
	Titles map[string]string `json:"titles"`
	// MidMerge reports whether each branch's own worktree is left mid-merge, read fresh every tick.
	MidMerge map[string]bool `json:"mid_merge"`
	// ConflictsWithBase reports whether origin/<branch> would conflict with origin/main.
	ConflictsWithBase map[string]bool `json:"conflicts_with_base"`
	// ConflictedPaths names each conflicting branch's own conflicted paths.
	ConflictedPaths map[string][]string `json:"conflicted_paths"`
	// ConflictsWithPeer reports whether two branches' tips would conflict if merged together,
	// keyed by branch key at both levels (docs/adr/0004-conflicts-resolve-once-and-one-peer-at-a-time.md).
	ConflictsWithPeer map[string]map[string]bool `json:"conflicts_with_peer"`
}
