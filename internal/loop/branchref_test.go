package loop_test

import "github.com/O-Marsters-1997/command-center/internal/plan"

func branchRef(repo, branch string) plan.BranchRef { return plan.BranchRef{Repo: repo, Branch: branch} }
