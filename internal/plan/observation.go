package plan

import "time"

type CheckState struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	DetailsURL string    `json:"details_url"`
	StartedAt  time.Time `json:"started_at"`
}

type PR struct {
	Number      int                   `json:"number"`
	HeadRef     string                `json:"head_ref"`
	HeadOid     SHA                   `json:"head_oid"`
	BaseRef     string                `json:"base_ref"`
	BaseOid     SHA                   `json:"base_oid"`
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

type Observation struct {
	ObservedAt        time.Time                        `json:"observed_at"`
	PRs               map[BranchRef]PR                 `json:"prs"`
	Worktrees         map[BranchRef]string             `json:"worktrees"`
	Runs              map[TicketURL]RunObservation     `json:"runs"`
	MergifyHash       map[string]string                `json:"mergify_hash"`
	BranchTips        map[BranchRef]SHA                `json:"branch_tips"`
	LocalTips         map[BranchRef]SHA                `json:"local_tips"`
	Titles            map[TicketURL]string             `json:"titles"`
	MidMerge          map[BranchRef]bool               `json:"mid_merge"`
	ConflictsWithBase map[BranchRef]bool               `json:"conflicts_with_base"`
	ConflictsWithPeer map[BranchRef]map[BranchRef]bool `json:"conflicts_with_peer"`
	Settings          map[string]RepoSettings          `json:"settings"`
	SettingsErrors    map[string]string                `json:"settings_errors"`
	SettingsSources   map[string]string                `json:"settings_sources"`
	RepoErrors        map[string]string                `json:"repo_errors"`
	Questions         map[RunID]string                 `json:"questions"`
}

func NewObservation(at time.Time) Observation {
	return Observation{
		ObservedAt: at,
		PRs:        map[BranchRef]PR{}, Worktrees: map[BranchRef]string{}, Runs: map[TicketURL]RunObservation{},
		MergifyHash: map[string]string{}, BranchTips: map[BranchRef]SHA{}, LocalTips: map[BranchRef]SHA{},
		Titles: map[TicketURL]string{}, MidMerge: map[BranchRef]bool{},
		ConflictsWithBase: map[BranchRef]bool{}, ConflictsWithPeer: map[BranchRef]map[BranchRef]bool{},
		Settings: map[string]RepoSettings{}, SettingsErrors: map[string]string{},
		SettingsSources: map[string]string{}, RepoErrors: map[string]string{}, Questions: map[RunID]string{},
	}
}

func (o *Observation) MarkAlive(url TicketURL) {
	o.Runs[url] = RunObservation{Alive: true}
}
