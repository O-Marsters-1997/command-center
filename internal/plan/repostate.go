package plan

import "strings"

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

func NormaliseRemote(url string) string {
	return strings.ToLower(HostPath(url))
}

func HostPath(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(strings.TrimSuffix(url, "/"), ".git")

	if scheme := strings.Index(url, "://"); scheme >= 0 {
		url = url[scheme+3:]
	} else if colon := strings.Index(url, ":"); colon >= 0 && !strings.Contains(url[:colon], "/") {
		url = url[:colon] + "/" + url[colon+1:]
	}
	if at := strings.Index(url, "@"); at >= 0 {
		url = url[at+1:]
	}
	return strings.TrimSuffix(url, "/")
}
