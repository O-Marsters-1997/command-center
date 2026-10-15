package plan

type RepoStatus struct {
	Name        string
	Phase       RepoPhase
	RefusalKind RefusalKind
	Refusal     string
}

func NextRepoStatus(current RepoStatus, obs Observation) RepoStatus {
	switch {
	case current.Phase == RepoCloning:
		return current
	case current.Phase == RepoRefused && current.RefusalKind != RefusalSettingsParse:
		return current
	}
	if reason, bad := obs.SettingsErrors[current.Name]; bad {
		return RepoStatus{Name: current.Name, Phase: RepoRefused, RefusalKind: RefusalSettingsParse, Refusal: reason}
	}
	if _, clean := obs.Settings[current.Name]; clean && current.Phase == RepoRefused {
		return RepoStatus{Name: current.Name, Phase: RepoReady}
	}
	return current
}
