package plan

import "time"

type CheckState struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	DetailsURL string    `json:"details_url"`
	StartedAt  time.Time `json:"started_at"`
}

// PR is one pull request as the tick observed it. The JSON tags are the persisted
// observation's shape, so a row saved before PR moved here still loads.
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

type RunObservation struct {
	Alive bool `json:"alive"`
}

// Observation is everything one tick read from the world, persisted so the page renders after
// a restart. It holds facts, never state labels. Branch-keyed maps are keyed repo + "//" + branch.
type Observation struct {
	ObservedAt        time.Time                  `json:"observed_at"`
	PRs               map[string]PR              `json:"prs"`
	Worktrees         map[string]string          `json:"worktrees"`
	Runs              map[string]RunObservation  `json:"runs"`
	MergifyHash       map[string]string          `json:"mergify_hash"`
	BranchTips        map[string]string          `json:"branch_tips"`
	LocalTips         map[string]string          `json:"local_tips"`
	Titles            map[string]string          `json:"titles"`
	MidMerge          map[string]bool            `json:"mid_merge"`
	ConflictsWithBase map[string]bool            `json:"conflicts_with_base"`
	ConflictsWithPeer map[string]map[string]bool `json:"conflicts_with_peer"`
}
