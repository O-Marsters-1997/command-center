package plan

// The kinds a refused repo carries.
const (
	RefusalMergeSettings = "merge_settings"
	RefusalDefaultBranch = "default_branch"
	RefusalClone         = "clone"
	RefusalSettingsParse = "settings_parse"
)

// RepoStatus is the part of a tracked repo the observation can move: its state and why it is
// refused. State is "cloning", "ready" or "refused".
type RepoStatus struct {
	Name        string
	State       string
	RefusalKind string
	Refusal     string
}

// NextRepoStatus applies this tick's settings read to a repo. A ready repo whose file would not
// read becomes refused(settings_parse), and that refusal alone heals when the file reads clean.
// Every other refusal waits for Track, and a repo with no read this tick stays as it was.
func NextRepoStatus(current RepoStatus, obs Observation) RepoStatus {
	switch {
	case current.State == "cloning":
		return current
	case current.State == "refused" && current.RefusalKind != RefusalSettingsParse:
		return current
	}
	if reason, bad := obs.SettingsErrors[current.Name]; bad {
		return RepoStatus{Name: current.Name, State: "refused", RefusalKind: RefusalSettingsParse, Refusal: reason}
	}
	if _, clean := obs.Settings[current.Name]; clean && current.State == "refused" {
		return RepoStatus{Name: current.Name, State: "ready"}
	}
	return current
}
