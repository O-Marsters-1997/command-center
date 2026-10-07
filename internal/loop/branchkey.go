package loop

import "github.com/O-Marsters-1997/command-center/internal/plan"

// defaultBaseBranch mirrors internal/plan's own unexported copy: verdict's import guard (like
// plan's) forbids depending on that package for one string constant.
const defaultBaseBranch = "main"

// branchKey names one branch in every branch-keyed map on Observation. Two configured repos can
// hold the same branch name, and "//" can never appear in a real git branch name, so this key
// never collides across repos the way the plain name would.
func branchKey(repo, branch string) string { return plan.BranchKey(repo, branch) }

// mainTipKey names defaultBaseBranch's own tip in Observation.BranchTips, main being every
// unstacked ticket's base (issue #85: main's own tip is checked exactly like a still-stacked
// base's, not exempted, since retargetOne can re-point a row onto it).
func mainTipKey(repo string) string { return branchKey(repo, defaultBaseBranch) }
