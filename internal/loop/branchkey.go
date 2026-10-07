package loop

import "github.com/O-Marsters-1997/command-center/internal/plan"

const defaultBaseBranch = "main"

func branchKey(repo, branch string) string { return plan.BranchKey(repo, branch) }

func mainTipKey(repo string) string { return branchKey(repo, defaultBaseBranch) }
