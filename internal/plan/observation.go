package plan

import (
	"maps"
	"time"
)

type CheckState struct {
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	DetailsURL  string    `json:"details_url"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Attempt     int       `json:"attempt"`
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
	Title       string                `json:"title"`
	URL         string                `json:"url"`
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
	HeadDescends      map[BranchRef]bool               `json:"head_descends"`
	Settings          map[string]RepoSettings          `json:"settings"`
	SettingsErrors    map[string]string                `json:"settings_errors"`
	SettingsSources   map[string]string                `json:"settings_sources"`
	RepoErrors        map[string]string                `json:"repo_errors"`
	Questions         map[RunID]string                 `json:"-"`
}

func NewObservation(at time.Time) Observation {
	return Observation{
		ObservedAt: at,
		PRs:        map[BranchRef]PR{}, Worktrees: map[BranchRef]string{}, Runs: map[TicketURL]RunObservation{},
		MergifyHash: map[string]string{}, BranchTips: map[BranchRef]SHA{}, LocalTips: map[BranchRef]SHA{},
		Titles: map[TicketURL]string{}, MidMerge: map[BranchRef]bool{},
		ConflictsWithBase: map[BranchRef]bool{}, ConflictsWithPeer: map[BranchRef]map[BranchRef]bool{},
		HeadDescends: map[BranchRef]bool{},
		Settings:     map[string]RepoSettings{}, SettingsErrors: map[string]string{},
		SettingsSources: map[string]string{}, RepoErrors: map[string]string{}, Questions: map[RunID]string{},
	}
}

func (o *Observation) Merge(src Observation) {
	maps.Copy(o.PRs, src.PRs)
	maps.Copy(o.Worktrees, src.Worktrees)
	maps.Copy(o.MergifyHash, src.MergifyHash)
	maps.Copy(o.BranchTips, src.BranchTips)
	maps.Copy(o.LocalTips, src.LocalTips)
	maps.Copy(o.MidMerge, src.MidMerge)
	maps.Copy(o.Titles, src.Titles)
	maps.Copy(o.ConflictsWithBase, src.ConflictsWithBase)
	maps.Copy(o.ConflictsWithPeer, src.ConflictsWithPeer)
	maps.Copy(o.HeadDescends, src.HeadDescends)
	maps.Copy(o.Settings, src.Settings)
	maps.Copy(o.SettingsErrors, src.SettingsErrors)
	maps.Copy(o.SettingsSources, src.SettingsSources)
}

func (o *Observation) CarryForward(prev Observation, repo string) {
	keep := func(key BranchRef) bool { return key.Repo == repo }
	copyKeyed(o.PRs, prev.PRs, keep)
	copyKeyed(o.Worktrees, prev.Worktrees, keep)
	copyKeyed(o.BranchTips, prev.BranchTips, keep)
	copyKeyed(o.LocalTips, prev.LocalTips, keep)
	copyKeyed(o.MidMerge, prev.MidMerge, keep)
	copyKeyed(o.ConflictsWithBase, prev.ConflictsWithBase, keep)
	copyKeyed(o.ConflictsWithPeer, prev.ConflictsWithPeer, keep)
	copyKeyed(o.HeadDescends, prev.HeadDescends, keep)
	copyKeyed(o.Titles, prev.Titles, func(TicketURL) bool { return true })
	named := func(key string) bool { return key == repo }
	copyKeyed(o.MergifyHash, prev.MergifyHash, named)
	copyKeyed(o.Settings, prev.Settings, named)
	copyKeyed(o.SettingsErrors, prev.SettingsErrors, named)
}

func copyKeyed[K comparable, V any](dst, src map[K]V, keep func(K) bool) {
	for key, value := range src {
		if _, taken := dst[key]; !taken && keep(key) {
			dst[key] = value
		}
	}
}

func (o *Observation) MarkAlive(url TicketURL) {
	o.Runs[url] = RunObservation{Alive: true}
}
