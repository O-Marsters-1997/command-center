package git

import (
	"context"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/gh"
	"github.com/O-Marsters-1997/command-center/internal/plan"
)

// CheckRepoOnGitHub reads GitHub merge settings before cloning; a GitHub error
// is not treated as a passing configuration check.
func CheckRepoOnGitHub(ctx context.Context, fullName string) (kind plan.RefusalKind, reason string, err error) {
	settings, err := gh.CLI{}.ReadRepoSettings(ctx, fullName)
	if err != nil {
		return "", "", fmt.Errorf("check repo %s on github: %w", fullName, err)
	}
	kind, reason = assertRepoSettings(fullName, settings)
	return kind, reason, nil
}

func assertRepoSettings(fullName string, settings gh.RepoSettings) (kind plan.RefusalKind, reason string) {
	switch {
	case settings.AllowMergeCommit:
		return plan.RefusalMergeSettings, fmt.Sprintf("repo %s allows merge commits (allow_merge_commit=true): "+
			"command-centre requires squash-only merges", fullName)
	case settings.AllowRebaseMerge:
		return plan.RefusalMergeSettings, fmt.Sprintf("repo %s allows rebase merges (allow_rebase_merge=true): "+
			"command-centre requires squash-only merges", fullName)
	case settings.DefaultBranch != plan.DefaultBaseBranch:
		return plan.RefusalDefaultBranch, fmt.Sprintf("default branch is `%s`, not `%s`; rename it on GitHub, "+
			"then Track again.", settings.DefaultBranch, plan.DefaultBaseBranch)
	}
	return "", ""
}
