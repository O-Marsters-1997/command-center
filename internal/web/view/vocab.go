package view

import (
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
	allLogFilters  = []LogFilter{LogAll, LogSkills, LogTools, LogFails}
	allSectionKeys = []SectionKey{SectionNeedsYou, SectionInProgress, SectionReady, SectionBlocked, SectionDone}
)

func ParseLogFilter(s string) LogFilter {
	if f := LogFilter(s); slices.Contains(allLogFilters, f) {
		return f
	}
	return LogAll
}
