package plan

type RepoStatus struct {
	Name        string
	Phase       RepoPhase
	RefusalKind RefusalKind
	Refusal     string
}

// NextRepoStatus applies this tick's settings read to a repo. A ready repo whose file would not
// read becomes refused(settings_parse), and that refusal alone heals when the file reads clean.
// Every other refusal waits for Track, and a repo with no read this tick stays as it was.
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
