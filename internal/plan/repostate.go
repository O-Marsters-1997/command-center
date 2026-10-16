package plan

import "fmt"

type MergeSettings struct {
	AllowMergeCommit bool
	AllowRebaseMerge bool
	DefaultBranch    string
}

func RepoRefusal(s MergeSettings) (kind RefusalKind, reason string) {
	switch {
	case s.AllowMergeCommit:
		return RefusalMergeSettings, "repo allows merge commits (allow_merge_commit=true): " +
			"command-centre requires squash-only merges"
	case s.AllowRebaseMerge:
		return RefusalMergeSettings, "repo allows rebase merges (allow_rebase_merge=true): " +
			"command-centre requires squash-only merges"
	case s.DefaultBranch != DefaultBaseBranch:
		return RefusalDefaultBranch, fmt.Sprintf("default branch is `%s`, not `%s`; rename it on GitHub, "+
			"then Track again.", s.DefaultBranch, DefaultBaseBranch)
	}
	return "", ""
}

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
