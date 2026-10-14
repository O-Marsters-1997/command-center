package view

import (
	"fmt"
	"slices"
)

type LogFilter string

const (
	LogFilterUnknown LogFilter = ""
	LogAll           LogFilter = "all"
	LogSkills        LogFilter = "skills"
	LogTools         LogFilter = "tools"
	LogFails         LogFilter = "fails"
)

type SectionKey string

const (
	SectionUnknown    SectionKey = ""
	SectionNeedsYou   SectionKey = "needs-you"
	SectionInProgress SectionKey = "in-progress"
	SectionReady      SectionKey = "ready"
	SectionBlocked    SectionKey = "blocked"
	SectionDone       SectionKey = "done"
)

type BannerState string

const (
	BannerUnknown   BannerState = ""
	BannerUntracked BannerState = "untracked"
	BannerCloning   BannerState = "cloning"
	BannerReady     BannerState = "ready"
	BannerRefused   BannerState = "refused"
)

var (
	allLogFilters   = []LogFilter{LogAll, LogSkills, LogTools, LogFails}
	allSectionKeys  = []SectionKey{SectionNeedsYou, SectionInProgress, SectionReady, SectionBlocked, SectionDone}
	allBannerStates = []BannerState{BannerUntracked, BannerCloning, BannerReady, BannerRefused}
)

func ParseLogFilter(s string) LogFilter {
	if f := LogFilter(s); slices.Contains(allLogFilters, f) {
		return f
	}
	return LogAll
}

func ParseSectionKey(s string) (SectionKey, error) {
	if k := SectionKey(s); slices.Contains(allSectionKeys, k) {
		return k, nil
	}
	return SectionUnknown, fmt.Errorf("unknown section key %q", s)
}

func ParseBannerState(s string) (BannerState, error) {
	if b := BannerState(s); slices.Contains(allBannerStates, b) {
		return b, nil
	}
	return BannerUnknown, fmt.Errorf("unknown banner state %q", s)
}
